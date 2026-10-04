use crate::model::ObjectId;
use crate::refs::RefLog;
use anyhow::Context;
use fs2::FileExt;
use serde::{Deserialize, Serialize};
use std::collections::BinaryHeap;
use std::collections::{BTreeMap, BTreeSet};
use std::fs::{self, File, OpenOptions};
use std::io::{ErrorKind, Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};

const MAGIC: &[u8; 4] = b"AGRT";
const MAX_RECORD: usize = 4 * 1024 * 1024;
const HOUR: i64 = 60 * 60;
const DAY: i64 = 24 * HOUR;
const WEEK: i64 = 7 * DAY;
const ALL_SNAPSHOTS_WINDOW: i64 = DAY;
const HOURLY_WINDOW: i64 = 7 * DAY;
const DAILY_WINDOW: i64 = 90 * DAY;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct SequenceRange {
    pub first: u64,
    pub last: u64,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Pin {
    pub sequence: u64,
    pub snapshot_id: ObjectId,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct RetentionState {
    pub event_sequence: u64,
    pub evaluated_through: u64,
    pub evaluated_at: i64,
    pub retained: Vec<SequenceRange>,
    pub pins: Vec<Pin>,
    pub previous_frame: ObjectId,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
enum Tier {
    Hour,
    Day,
    Week,
}

#[derive(Debug, Clone, Copy)]
struct Candidate {
    sealed_at: i64,
    sequence: u64,
}

pub struct RetentionLog {
    path: PathBuf,
    lock_path: PathBuf,
}

impl RetentionState {
    fn initial() -> Self {
        Self {
            event_sequence: 0,
            evaluated_through: 0,
            evaluated_at: 0,
            retained: Vec::new(),
            pins: Vec::new(),
            previous_frame: [0; 32],
        }
    }

    pub fn retains(&self, sequence: u64, snapshot_id: &ObjectId) -> bool {
        sequence > self.evaluated_through
            || self.retains_in_ranges(sequence)
            || self
                .pins
                .binary_search_by_key(&sequence, |pin| pin.sequence)
                .is_ok()
            || self.pins.iter().any(|pin| pin.snapshot_id == *snapshot_id)
    }

    pub fn is_pinned(&self, snapshot_id: &ObjectId) -> bool {
        self.pins.iter().any(|pin| pin.snapshot_id == *snapshot_id)
    }

    pub fn retained_count_through(&self) -> u64 {
        self.retained
            .iter()
            .map(|range| range.last - range.first + 1)
            .sum()
    }

    pub fn retained_count(&self, head: u64) -> u64 {
        let mut count = self.retained_count_through();
        if head > self.evaluated_through {
            count = count.saturating_add(head - self.evaluated_through);
        }
        for pin in &self.pins {
            if pin.sequence <= self.evaluated_through && !self.retains_in_ranges(pin.sequence) {
                count = count.saturating_add(1);
            }
        }
        count
    }

    fn retains_in_ranges(&self, sequence: u64) -> bool {
        self.retained
            .binary_search_by(|range| {
                if sequence < range.first {
                    std::cmp::Ordering::Greater
                } else if sequence > range.last {
                    std::cmp::Ordering::Less
                } else {
                    std::cmp::Ordering::Equal
                }
            })
            .is_ok()
    }

    pub fn recent_sequences(&self, head: u64, limit: usize) -> Vec<u64> {
        if limit == 0 || head == 0 {
            return Vec::new();
        }
        let mut intervals = BinaryHeap::new();
        if head > self.evaluated_through {
            intervals.push((head, self.evaluated_through + 1));
        }
        for range in &self.retained {
            intervals.push((range.last.min(head), range.first));
        }
        for pin in &self.pins {
            if pin.sequence <= head {
                intervals.push((pin.sequence, pin.sequence));
            }
        }

        let mut result = Vec::with_capacity(limit.min(1024));
        let mut previous = None;
        while result.len() < limit {
            let Some((sequence, first)) = intervals.pop() else {
                break;
            };
            if sequence >= first {
                if previous != Some(sequence) {
                    result.push(sequence);
                    previous = Some(sequence);
                }
                if sequence > first {
                    intervals.push((sequence - 1, first));
                }
            }
        }
        result
    }
}

impl RetentionLog {
    pub fn open(store_root: &Path, workspace_id: &str) -> anyhow::Result<Self> {
        let workspace_dir = store_root.join("workspaces").join(workspace_id);
        fs::create_dir_all(&workspace_dir)?;
        Ok(Self {
            path: workspace_dir.join("retention.log"),
            lock_path: workspace_dir.join("retention.lock"),
        })
    }

    pub fn state(&self) -> anyhow::Result<RetentionState> {
        let lock = self.lock()?;
        let mut file = self.open_log()?;
        let state = read_latest(&mut file)?;
        FileExt::unlock(&lock)?;
        Ok(state)
    }

    pub fn plan(&self, refs: &RefLog, now: i64) -> anyhow::Result<RetentionState> {
        let current = self.state()?;
        plan_state(refs, now, current.pins)
    }

    pub fn apply(&self, refs: &RefLog, now: i64) -> anyhow::Result<RetentionState> {
        let lock = self.lock()?;
        let mut file = self.open_log()?;
        let current = read_latest(&mut file)?;
        let mut planned = plan_state(refs, now, current.pins.clone())?;
        if same_policy_state(&current, &planned) {
            FileExt::unlock(&lock)?;
            return Ok(current);
        }
        planned.event_sequence = current.event_sequence + 1;
        planned.previous_frame = current_frame_hash(&mut file)?.unwrap_or([0; 32]);
        append_state(&mut file, &planned)?;
        FileExt::unlock(&lock)?;
        Ok(planned)
    }

    pub fn pin(&self, refs: &RefLog, snapshot_id: ObjectId) -> anyhow::Result<bool> {
        let record = refs
            .find(&snapshot_id)?
            .context("snapshot does not belong to this workspace timeline")?;
        let lock = self.lock()?;
        let mut file = self.open_log()?;
        let mut state = read_latest(&mut file)?;
        if state.is_pinned(&snapshot_id) {
            FileExt::unlock(&lock)?;
            return Ok(false);
        }
        state.pins.push(Pin {
            sequence: record.sequence,
            snapshot_id,
        });
        state.pins.sort_by_key(|pin| pin.sequence);
        state.event_sequence += 1;
        state.previous_frame = current_frame_hash(&mut file)?.unwrap_or([0; 32]);
        append_state(&mut file, &state)?;
        FileExt::unlock(&lock)?;
        Ok(true)
    }

    pub fn unpin(&self, snapshot_id: &ObjectId) -> anyhow::Result<bool> {
        let lock = self.lock()?;
        let mut file = self.open_log()?;
        let mut state = read_latest(&mut file)?;
        let before = state.pins.len();
        state.pins.retain(|pin| pin.snapshot_id != *snapshot_id);
        if state.pins.len() == before {
            FileExt::unlock(&lock)?;
            return Ok(false);
        }
        state.event_sequence += 1;
        state.previous_frame = current_frame_hash(&mut file)?.unwrap_or([0; 32]);
        append_state(&mut file, &state)?;
        FileExt::unlock(&lock)?;
        Ok(true)
    }

    fn lock(&self) -> anyhow::Result<File> {
        let lock = OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(&self.lock_path)?;
        lock.lock_exclusive()?;
        Ok(lock)
    }

    fn open_log(&self) -> anyhow::Result<File> {
        Ok(OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(&self.path)?)
    }
}

fn plan_state(refs: &RefLog, now: i64, pins: Vec<Pin>) -> anyhow::Result<RetentionState> {
    let pinned: BTreeSet<_> = pins.iter().map(|pin| pin.sequence).collect();
    let mut retained = BTreeSet::new();
    let mut buckets: BTreeMap<(Tier, i64), Candidate> = BTreeMap::new();
    let mut evaluated_through = 0_u64;

    refs.for_each_record(|record| {
        evaluated_through = record.sequence;
        let age = now.saturating_sub(record.sealed_at);
        if age <= ALL_SNAPSHOTS_WINDOW || age < 0 || pinned.contains(&record.sequence) {
            retained.insert(record.sequence);
            return Ok(());
        }
        let (tier, width) = if age <= HOURLY_WINDOW {
            (Tier::Hour, HOUR)
        } else if age <= DAILY_WINDOW {
            (Tier::Day, DAY)
        } else {
            (Tier::Week, WEEK)
        };
        let key = (tier, record.sealed_at.div_euclid(width));
        let candidate = Candidate {
            sealed_at: record.sealed_at,
            sequence: record.sequence,
        };
        buckets
            .entry(key)
            .and_modify(|existing| {
                if (candidate.sealed_at, candidate.sequence)
                    > (existing.sealed_at, existing.sequence)
                {
                    *existing = candidate;
                }
            })
            .or_insert(candidate);
        Ok(())
    })?;

    retained.extend(buckets.values().map(|candidate| candidate.sequence));
    retained.extend(pinned);
    if evaluated_through > 0 {
        retained.insert(evaluated_through);
    }
    Ok(RetentionState {
        event_sequence: 0,
        evaluated_through,
        evaluated_at: now,
        retained: compress_sequences(retained),
        pins,
        previous_frame: [0; 32],
    })
}

fn compress_sequences(sequences: BTreeSet<u64>) -> Vec<SequenceRange> {
    let mut ranges: Vec<SequenceRange> = Vec::new();
    for sequence in sequences {
        if let Some(last) = ranges.last_mut() {
            if last.last.checked_add(1) == Some(sequence) {
                last.last = sequence;
                continue;
            }
        }
        ranges.push(SequenceRange {
            first: sequence,
            last: sequence,
        });
    }
    ranges
}

fn same_policy_state(left: &RetentionState, right: &RetentionState) -> bool {
    left.evaluated_through == right.evaluated_through
        && left.retained == right.retained
        && left.pins == right.pins
}

fn append_state(file: &mut File, state: &RetentionState) -> anyhow::Result<()> {
    let payload = serde_json::to_vec(state)?;
    anyhow::ensure!(payload.len() <= MAX_RECORD, "retention record is too large");
    let checksum = blake3::hash(&payload);
    file.seek(SeekFrom::End(0))?;
    file.write_all(MAGIC)?;
    file.write_all(&(payload.len() as u32).to_le_bytes())?;
    file.write_all(checksum.as_bytes())?;
    file.write_all(&payload)?;
    file.sync_data()?;
    Ok(())
}

fn read_latest(file: &mut File) -> anyhow::Result<RetentionState> {
    file.seek(SeekFrom::Start(0))?;
    let file_len = file.metadata()?.len();
    let mut offset = 0_u64;
    let mut state = RetentionState::initial();
    let mut previous = [0_u8; 32];
    while offset < file_len {
        let start = offset;
        let Some((next, frame_hash, end)) = read_frame(file, start)? else {
            file.set_len(start)?;
            file.sync_data()?;
            break;
        };
        anyhow::ensure!(
            next.event_sequence == state.event_sequence + 1,
            "broken retention sequence"
        );
        anyhow::ensure!(
            next.previous_frame == previous,
            "broken retention hash chain"
        );
        validate_state(&next)?;
        state = next;
        previous = frame_hash;
        offset = end;
    }
    Ok(state)
}

fn current_frame_hash(file: &mut File) -> anyhow::Result<Option<ObjectId>> {
    file.seek(SeekFrom::Start(0))?;
    let file_len = file.metadata()?.len();
    let mut offset = 0_u64;
    let mut latest = None;
    while offset < file_len {
        let Some((_, frame_hash, end)) = read_frame(file, offset)? else {
            return Ok(latest);
        };
        latest = Some(frame_hash);
        offset = end;
    }
    Ok(latest)
}

fn read_frame(
    file: &mut File,
    start: u64,
) -> anyhow::Result<Option<(RetentionState, ObjectId, u64)>> {
    file.seek(SeekFrom::Start(start))?;
    let mut magic = [0; 4];
    if let Err(error) = file.read_exact(&mut magic) {
        return if error.kind() == ErrorKind::UnexpectedEof {
            Ok(None)
        } else {
            Err(error.into())
        };
    }
    anyhow::ensure!(&magic == MAGIC, "retention log corruption at byte {start}");
    let mut len_bytes = [0; 4];
    if file.read_exact(&mut len_bytes).is_err() {
        return Ok(None);
    }
    let len = u32::from_le_bytes(len_bytes) as usize;
    anyhow::ensure!(len <= MAX_RECORD, "retention record exceeds size limit");
    let mut checksum = [0; 32];
    if file.read_exact(&mut checksum).is_err() {
        return Ok(None);
    }
    let mut payload = vec![0; len];
    if file.read_exact(&mut payload).is_err() {
        return Ok(None);
    }
    anyhow::ensure!(
        blake3::hash(&payload).as_bytes() == &checksum,
        "retention checksum mismatch"
    );
    let state = serde_json::from_slice(&payload).context("decode retention record")?;
    let end = file.stream_position()?;
    let mut hasher = blake3::Hasher::new();
    hasher.update(MAGIC);
    hasher.update(&len_bytes);
    hasher.update(&checksum);
    hasher.update(&payload);
    Ok(Some((state, *hasher.finalize().as_bytes(), end)))
}

fn validate_state(state: &RetentionState) -> anyhow::Result<()> {
    let mut previous = 0_u64;
    for range in &state.retained {
        anyhow::ensure!(
            range.first > 0 && range.first <= range.last,
            "invalid retained sequence range"
        );
        anyhow::ensure!(range.first > previous, "overlapping retained ranges");
        anyhow::ensure!(
            range.last <= state.evaluated_through,
            "retained range exceeds evaluated history"
        );
        previous = range.last;
    }
    let mut previous_pin = 0_u64;
    for pin in &state.pins {
        anyhow::ensure!(pin.sequence > previous_pin, "pins are not ordered");
        previous_pin = pin.sequence;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::SnapshotTrigger;

    fn append(log: &RefLog, sequence: u64, sealed_at: i64) -> ObjectId {
        let id = blake3::hash(&sequence.to_le_bytes()).into();
        log.append(id, sealed_at, None, SnapshotTrigger::Manual)
            .unwrap();
        id
    }

    #[test]
    fn six_month_plan_keeps_dense_recent_and_thins_old_history() {
        let temp = tempfile::tempdir().unwrap();
        let refs = RefLog::open(temp.path(), "workspace").unwrap();
        let now = 200 * DAY;
        let interval = 6 * HOUR;
        let first = now - 180 * DAY;
        let count = 180 * DAY / interval;
        for sequence in 0..=count {
            append(&refs, sequence as u64 + 1, first + sequence * interval);
        }

        let state = RetentionLog::open(temp.path(), "workspace")
            .unwrap()
            .plan(&refs, now)
            .unwrap();
        assert_eq!(state.evaluated_through, count as u64 + 1);
        assert!(state.retained_count_through() < 300);
        assert!(state.retains(state.evaluated_through, &[0; 32]));
        let recent_start = state.evaluated_through - (DAY / interval) as u64;
        for sequence in recent_start..=state.evaluated_through {
            assert!(state.retains(sequence, &[0; 32]));
        }
    }

    #[test]
    fn pin_survives_thinning_and_torn_control_tail() {
        let temp = tempfile::tempdir().unwrap();
        let refs = RefLog::open(temp.path(), "workspace").unwrap();
        let old = append(&refs, 1, 0);
        append(&refs, 2, WEEK);
        let retention = RetentionLog::open(temp.path(), "workspace").unwrap();
        assert!(retention.pin(&refs, old).unwrap());
        let applied = retention.apply(&refs, 200 * DAY).unwrap();
        assert!(applied.is_pinned(&old));
        let valid_len = fs::metadata(&retention.path).unwrap().len();
        OpenOptions::new()
            .append(true)
            .open(&retention.path)
            .unwrap()
            .write_all(b"AG")
            .unwrap();
        let recovered = retention.state().unwrap();
        assert!(recovered.is_pinned(&old));
        assert_eq!(fs::metadata(&retention.path).unwrap().len(), valid_len);
    }
}
