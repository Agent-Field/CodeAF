use crate::model::{ObjectId, SnapshotTrigger};
use anyhow::Context;
use fs2::FileExt;
use serde::{Deserialize, Serialize};
use std::fs::{self, File, Metadata, OpenOptions};
use std::io::{ErrorKind, Read, Seek, SeekFrom, Write};
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};

const MAGIC: &[u8; 4] = b"AGRF";
const INDEX_MAGIC: &[u8; 8] = b"AGRI\x01\0\0\0";
const MAX_RECORD: usize = 1024 * 1024;
const INDEX_FIELDS_LEN: usize = 8 * 8 + 32;
const INDEX_RECORD_LEN: usize = INDEX_FIELDS_LEN + 32;
type DecodedRecords = (Vec<DecodedRecord>, u64, bool);

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RefRecord {
    pub sequence: u64,
    pub snapshot_id: ObjectId,
    pub sealed_at: i64,
    pub label: Option<String>,
    pub trigger: SnapshotTrigger,
    pub previous_frame: ObjectId,
}

#[derive(Debug, Clone)]
struct DecodedRecord {
    record: RefRecord,
    frame_hash: ObjectId,
    start: u64,
    end: u64,
}

#[derive(Debug, Clone, Copy)]
struct IndexRecord {
    start: u64,
    end: u64,
    frame_hash: ObjectId,
    device: u64,
    inode: u64,
    mtime_secs: i64,
    mtime_nanos: i64,
    ctime_secs: i64,
    ctime_nanos: i64,
}

pub struct RefLog {
    path: PathBuf,
    index_path: PathBuf,
    lock_path: PathBuf,
}

impl RefLog {
    pub fn open(store_root: &Path, workspace_id: &str) -> anyhow::Result<Self> {
        let workspace_dir = store_root.join("workspaces").join(workspace_id);
        fs::create_dir_all(&workspace_dir)?;
        Ok(Self {
            path: workspace_dir.join("refs.log"),
            index_path: workspace_dir.join("refs.index"),
            lock_path: workspace_dir.join("refs.lock"),
        })
    }

    /// Reads and verifies the complete authoritative log. This is intentionally
    /// the recovery/rebuild path; normal head and timeline reads use the index.
    pub fn records(&self) -> anyhow::Result<Vec<RefRecord>> {
        let lock = self.lock()?;
        let mut file = self.open_log()?;
        let (records, valid_len, incomplete_tail) = read_records(&mut file, 0, [0; 32], 1)?;
        if incomplete_tail {
            file.set_len(valid_len)?;
            file.sync_data()?;
        }
        self.rewrite_index(&file, &records)?;
        FileExt::unlock(&lock)?;
        Ok(records.into_iter().map(|decoded| decoded.record).collect())
    }

    pub fn head(&self) -> anyhow::Result<Option<RefRecord>> {
        Ok(self.recent(1)?.pop())
    }

    /// Streams every authoritative snapshot reference in log order using
    /// constant memory. Unlike the reverse index fast path, this verifies every
    /// frame and the complete hash chain before returning.
    pub fn for_each_snapshot(
        &self,
        mut visit: impl FnMut(ObjectId) -> anyhow::Result<()>,
    ) -> anyhow::Result<()> {
        self.for_each_record(|record| visit(record.snapshot_id))
    }

    pub fn for_each_record(
        &self,
        mut visit: impl FnMut(&RefRecord) -> anyhow::Result<()>,
    ) -> anyhow::Result<()> {
        if !self.path.exists() {
            return Ok(());
        }
        let lock = self.lock()?;
        let mut log = self.open_log()?;
        self.ensure_index(&mut log)?;
        let length = log.metadata()?.len();
        let mut offset = 0_u64;
        let mut previous = [0_u8; 32];
        let mut sequence = 1_u64;
        while offset < length {
            let frame = read_frame_at(&mut log, offset)?
                .context("reference log ended before its indexed boundary")?;
            anyhow::ensure!(
                frame.record.sequence == sequence,
                "non-monotonic reference sequence"
            );
            anyhow::ensure!(
                frame.record.previous_frame == previous,
                "broken reference hash chain"
            );
            visit(&frame.record)?;
            offset = frame.end;
            previous = frame.frame_hash;
            sequence += 1;
        }
        FileExt::unlock(&lock)?;
        Ok(())
    }

    pub fn find(&self, snapshot_id: &ObjectId) -> anyhow::Result<Option<RefRecord>> {
        let mut found = None;
        self.for_each_record(|record| {
            if record.snapshot_id == *snapshot_id {
                found = Some(record.clone());
            }
            Ok(())
        })?;
        Ok(found)
    }

    pub fn records_at_sequences(&self, sequences: &[u64]) -> anyhow::Result<Vec<RefRecord>> {
        if sequences.is_empty() || !self.path.exists() {
            return Ok(Vec::new());
        }
        let lock = self.lock()?;
        let mut log = self.open_log()?;
        self.ensure_index(&mut log)?;
        let mut index = File::open(&self.index_path)?;
        let count = (index.metadata()?.len() - INDEX_MAGIC.len() as u64) / INDEX_RECORD_LEN as u64;
        let mut records = Vec::with_capacity(sequences.len());
        for sequence in sequences {
            anyhow::ensure!(
                *sequence > 0 && *sequence <= count,
                "reference sequence is outside the indexed log"
            );
            index.seek(SeekFrom::Start(
                INDEX_MAGIC.len() as u64 + (*sequence - 1) * INDEX_RECORD_LEN as u64,
            ))?;
            let indexed = read_index_record(&mut index)?;
            let frame = read_frame_at(&mut log, indexed.start)?
                .context("reference index points beyond the log")?;
            anyhow::ensure!(
                frame.record.sequence == *sequence
                    && frame.end == indexed.end
                    && frame.frame_hash == indexed.frame_hash,
                "reference index does not match the authoritative log"
            );
            records.push(frame.record);
        }
        FileExt::unlock(&lock)?;
        Ok(records)
    }

    /// Returns newest-first records while reading only O(limit) log frames once
    /// the reverse index has been validated.
    pub fn recent(&self, limit: usize) -> anyhow::Result<Vec<RefRecord>> {
        if limit == 0 || !self.path.exists() {
            return Ok(Vec::new());
        }
        let lock = self.lock()?;
        let mut log = self.open_log()?;
        self.ensure_index(&mut log)?;
        let records = match self.read_recent_indexed(&mut log, limit) {
            Ok(records) => records,
            Err(_) => {
                self.rebuild_index(&mut log)?;
                self.read_recent_indexed(&mut log, limit)?
            }
        };
        FileExt::unlock(&lock)?;
        Ok(records)
    }

    fn read_recent_indexed(&self, log: &mut File, limit: usize) -> anyhow::Result<Vec<RefRecord>> {
        let entries = self.read_index_tail(limit.saturating_add(1))?;
        let skip = usize::from(entries.len() > limit);
        let mut decoded = Vec::with_capacity(entries.len().saturating_sub(skip));
        for entry in &entries[skip..] {
            let frame = read_frame_at(log, entry.start)?
                .context("reference index points beyond the log")?;
            anyhow::ensure!(
                frame.end == entry.end && frame.frame_hash == entry.frame_hash,
                "reference index does not match the authoritative log"
            );
            decoded.push(frame);
        }
        if let Some(first) = decoded.first() {
            let expected = if skip == 1 {
                entries[0].frame_hash
            } else {
                [0; 32]
            };
            anyhow::ensure!(
                first.record.previous_frame == expected,
                "broken reference hash chain"
            );
        }
        for pair in decoded.windows(2) {
            anyhow::ensure!(
                pair[1].record.previous_frame == pair[0].frame_hash,
                "broken reference hash chain"
            );
        }
        Ok(decoded
            .into_iter()
            .rev()
            .map(|frame| frame.record)
            .collect())
    }

    pub fn append(
        &self,
        snapshot_id: ObjectId,
        sealed_at: i64,
        label: Option<String>,
        trigger: SnapshotTrigger,
    ) -> anyhow::Result<RefRecord> {
        let lock = self.lock()?;
        let mut file = self.open_log()?;
        self.ensure_index(&mut file)?;
        let last = self.read_index_tail(1)?.pop();
        let last_frame = last
            .map(|entry| read_frame_at(&mut file, entry.start))
            .transpose()?
            .flatten();
        if let Some(existing) = &last_frame {
            if existing.record.snapshot_id == snapshot_id {
                FileExt::unlock(&lock)?;
                return Ok(existing.record.clone());
            }
        }

        let sequence = last_frame
            .as_ref()
            .map_or(1, |frame| frame.record.sequence + 1);
        let previous_frame = last_frame
            .as_ref()
            .map_or([0; 32], |frame| frame.frame_hash);
        let record = RefRecord {
            sequence,
            snapshot_id,
            sealed_at,
            label,
            trigger,
            previous_frame,
        };
        let payload = serde_json::to_vec(&record)?;
        anyhow::ensure!(payload.len() <= MAX_RECORD, "reference record is too large");
        let checksum = blake3::hash(&payload);
        let len_bytes = (payload.len() as u32).to_le_bytes();
        let start = file.seek(SeekFrom::End(0))?;
        file.write_all(MAGIC)?;
        file.write_all(&len_bytes)?;
        file.write_all(checksum.as_bytes())?;
        file.write_all(&payload)?;
        file.sync_data()?;
        let end = file.stream_position()?;
        let frame_hash = hash_frame(&len_bytes, checksum.as_bytes(), &payload);
        self.append_index(IndexRecord::new(start, end, frame_hash, &file.metadata()?))?;
        FileExt::unlock(&lock)?;
        Ok(record)
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

    fn ensure_index(&self, log: &mut File) -> anyhow::Result<()> {
        let metadata = log.metadata()?;
        let last = match self.prepare_index() {
            Ok(last) => last,
            Err(_) => return self.rebuild_index(log),
        };
        let Some(last) = last else {
            if metadata.len() == 0 {
                return self.write_empty_index();
            }
            return self.rebuild_index(log);
        };

        if last.end > metadata.len()
            || last.device != metadata.dev()
            || last.inode != metadata.ino()
        {
            return self.rebuild_index(log);
        }
        let Some(frame) = read_frame_at(log, last.start)? else {
            return self.rebuild_index(log);
        };
        if frame.end != last.end || frame.frame_hash != last.frame_hash {
            return self.rebuild_index(log);
        }
        if metadata.len() == last.end {
            if !last.matches_metadata(&metadata) {
                return self.rebuild_index(log);
            }
            return Ok(());
        }

        let (suffix, valid_len, incomplete_tail) =
            read_records(log, last.end, last.frame_hash, frame.record.sequence + 1)?;
        if incomplete_tail {
            log.set_len(valid_len)?;
            log.sync_data()?;
        }
        if suffix.is_empty() {
            self.refresh_index_tail(&IndexRecord::new(
                last.start,
                last.end,
                last.frame_hash,
                &log.metadata()?,
            ))
        } else {
            self.append_index_records(log, &suffix)
        }
    }

    fn rebuild_index(&self, log: &mut File) -> anyhow::Result<()> {
        let (records, valid_len, incomplete_tail) = read_records(log, 0, [0; 32], 1)?;
        if incomplete_tail {
            log.set_len(valid_len)?;
            log.sync_data()?;
        }
        self.rewrite_index(log, &records)
    }

    fn prepare_index(&self) -> anyhow::Result<Option<IndexRecord>> {
        if !self.index_path.exists() {
            anyhow::bail!("reference index is missing");
        }
        let mut file = OpenOptions::new()
            .read(true)
            .write(true)
            .open(&self.index_path)?;
        let mut header = [0; INDEX_MAGIC.len()];
        file.read_exact(&mut header)?;
        anyhow::ensure!(&header == INDEX_MAGIC, "invalid reference index header");
        let data_len = file.metadata()?.len() - INDEX_MAGIC.len() as u64;
        let complete_len = data_len / INDEX_RECORD_LEN as u64 * INDEX_RECORD_LEN as u64;
        if complete_len != data_len {
            file.set_len(INDEX_MAGIC.len() as u64 + complete_len)?;
            file.sync_data()?;
        }
        if complete_len == 0 {
            return Ok(None);
        }
        file.seek(SeekFrom::Start(
            INDEX_MAGIC.len() as u64 + complete_len - INDEX_RECORD_LEN as u64,
        ))?;
        Ok(Some(read_index_record(&mut file)?))
    }

    fn read_index_tail(&self, limit: usize) -> anyhow::Result<Vec<IndexRecord>> {
        let mut file = File::open(&self.index_path)?;
        let count = (file.metadata()?.len() - INDEX_MAGIC.len() as u64) / INDEX_RECORD_LEN as u64;
        let take = count.min(limit as u64);
        file.seek(SeekFrom::Start(
            INDEX_MAGIC.len() as u64 + (count - take) * INDEX_RECORD_LEN as u64,
        ))?;
        (0..take).map(|_| read_index_record(&mut file)).collect()
    }

    fn rewrite_index(&self, log: &File, records: &[DecodedRecord]) -> anyhow::Result<()> {
        let temporary = self.index_path.with_extension("index.tmp");
        let mut index = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .open(&temporary)?;
        index.write_all(INDEX_MAGIC)?;
        let metadata = log.metadata()?;
        for record in records {
            write_index_record(
                &mut index,
                &IndexRecord::new(record.start, record.end, record.frame_hash, &metadata),
            )?;
        }
        index.sync_all()?;
        fs::rename(temporary, &self.index_path)?;
        File::open(self.index_path.parent().context("index has no parent")?)?.sync_all()?;
        Ok(())
    }

    fn write_empty_index(&self) -> anyhow::Result<()> {
        self.rewrite_index(&self.open_log()?, &[])
    }

    fn append_index_records(&self, log: &File, records: &[DecodedRecord]) -> anyhow::Result<()> {
        let metadata = log.metadata()?;
        for record in records {
            self.append_index(IndexRecord::new(
                record.start,
                record.end,
                record.frame_hash,
                &metadata,
            ))?;
        }
        Ok(())
    }

    fn append_index(&self, record: IndexRecord) -> anyhow::Result<()> {
        let mut index = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.index_path)?;
        if index.metadata()?.len() == 0 {
            index.write_all(INDEX_MAGIC)?;
        }
        write_index_record(&mut index, &record)?;
        index.sync_data()?;
        Ok(())
    }

    fn refresh_index_tail(&self, record: &IndexRecord) -> anyhow::Result<()> {
        let mut index = OpenOptions::new()
            .read(true)
            .write(true)
            .open(&self.index_path)?;
        let len = index.metadata()?.len();
        anyhow::ensure!(
            len >= INDEX_MAGIC.len() as u64 + INDEX_RECORD_LEN as u64,
            "reference index has no tail"
        );
        index.seek(SeekFrom::Start(len - INDEX_RECORD_LEN as u64))?;
        write_index_record(&mut index, record)?;
        index.sync_data()?;
        Ok(())
    }
}

impl IndexRecord {
    fn new(start: u64, end: u64, frame_hash: ObjectId, metadata: &Metadata) -> Self {
        Self {
            start,
            end,
            frame_hash,
            device: metadata.dev(),
            inode: metadata.ino(),
            mtime_secs: metadata.mtime(),
            mtime_nanos: metadata.mtime_nsec(),
            ctime_secs: metadata.ctime(),
            ctime_nanos: metadata.ctime_nsec(),
        }
    }

    fn matches_metadata(&self, metadata: &Metadata) -> bool {
        self.end == metadata.len()
            && self.device == metadata.dev()
            && self.inode == metadata.ino()
            && self.mtime_secs == metadata.mtime()
            && self.mtime_nanos == metadata.mtime_nsec()
            && self.ctime_secs == metadata.ctime()
            && self.ctime_nanos == metadata.ctime_nsec()
    }
}

fn read_records(
    file: &mut File,
    start: u64,
    mut expected_previous: ObjectId,
    mut expected_sequence: u64,
) -> anyhow::Result<DecodedRecords> {
    file.seek(SeekFrom::Start(start))?;
    let file_len = file.metadata()?.len();
    let mut offset = start;
    let mut records = Vec::new();

    while offset < file_len {
        let frame_start = offset;
        let Some(decoded) = read_frame_at(file, frame_start)? else {
            return Ok((records, frame_start, true));
        };
        anyhow::ensure!(
            decoded.record.previous_frame == expected_previous,
            "broken reference hash chain"
        );
        anyhow::ensure!(
            decoded.record.sequence == expected_sequence,
            "broken reference sequence"
        );
        expected_previous = decoded.frame_hash;
        expected_sequence += 1;
        offset = decoded.end;
        records.push(decoded);
    }
    Ok((records, offset, false))
}

fn read_frame_at(file: &mut File, start: u64) -> anyhow::Result<Option<DecodedRecord>> {
    file.seek(SeekFrom::Start(start))?;
    let mut magic = [0; 4];
    if let Err(error) = file.read_exact(&mut magic) {
        return if error.kind() == ErrorKind::UnexpectedEof {
            Ok(None)
        } else {
            Err(error.into())
        };
    }
    anyhow::ensure!(&magic == MAGIC, "reference log corruption at byte {start}");
    let mut len_bytes = [0; 4];
    if file.read_exact(&mut len_bytes).is_err() {
        return Ok(None);
    }
    let len = u32::from_le_bytes(len_bytes) as usize;
    anyhow::ensure!(len <= MAX_RECORD, "reference record exceeds size limit");
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
        "reference checksum mismatch"
    );
    let record: RefRecord = serde_json::from_slice(&payload).context("decode reference record")?;
    let end = file.stream_position()?;
    Ok(Some(DecodedRecord {
        record,
        frame_hash: hash_frame(&len_bytes, &checksum, &payload),
        start,
        end,
    }))
}

fn hash_frame(len: &[u8; 4], checksum: &[u8; 32], payload: &[u8]) -> ObjectId {
    let mut hasher = blake3::Hasher::new();
    hasher.update(MAGIC);
    hasher.update(len);
    hasher.update(checksum);
    hasher.update(payload);
    *hasher.finalize().as_bytes()
}

fn write_index_record(file: &mut File, record: &IndexRecord) -> anyhow::Result<()> {
    let fields = encode_index_fields(record);
    file.write_all(&fields)?;
    file.write_all(blake3::hash(&fields).as_bytes())?;
    Ok(())
}

fn read_index_record(file: &mut File) -> anyhow::Result<IndexRecord> {
    let mut fields = [0; INDEX_FIELDS_LEN];
    let mut checksum = [0; 32];
    file.read_exact(&mut fields)?;
    file.read_exact(&mut checksum)?;
    anyhow::ensure!(
        blake3::hash(&fields).as_bytes() == &checksum,
        "reference index checksum mismatch"
    );
    decode_index_fields(&fields)
}

fn encode_index_fields(record: &IndexRecord) -> [u8; INDEX_FIELDS_LEN] {
    let mut bytes = [0; INDEX_FIELDS_LEN];
    bytes[0..8].copy_from_slice(&record.start.to_le_bytes());
    bytes[8..16].copy_from_slice(&record.end.to_le_bytes());
    bytes[16..48].copy_from_slice(&record.frame_hash);
    for (slot, value) in [
        record.device as i64,
        record.inode as i64,
        record.mtime_secs,
        record.mtime_nanos,
        record.ctime_secs,
        record.ctime_nanos,
    ]
    .into_iter()
    .enumerate()
    {
        let start = 48 + slot * 8;
        bytes[start..start + 8].copy_from_slice(&value.to_le_bytes());
    }
    bytes
}

fn decode_index_fields(bytes: &[u8; INDEX_FIELDS_LEN]) -> anyhow::Result<IndexRecord> {
    let read_i64 = |start: usize| i64::from_le_bytes(bytes[start..start + 8].try_into().unwrap());
    let mut frame_hash = [0; 32];
    frame_hash.copy_from_slice(&bytes[16..48]);
    let start = read_i64(0);
    let end = read_i64(8);
    anyhow::ensure!(
        start >= 0 && end >= start,
        "invalid reference index offsets"
    );
    Ok(IndexRecord {
        start: start as u64,
        end: end as u64,
        frame_hash,
        device: read_i64(48) as u64,
        inode: read_i64(56) as u64,
        mtime_secs: read_i64(64),
        mtime_nanos: read_i64(72),
        ctime_secs: read_i64(80),
        ctime_nanos: read_i64(88),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn snapshot(byte: u8) -> ObjectId {
        [byte; 32]
    }

    fn append(log: &RefLog, byte: u8) -> RefRecord {
        log.append(
            snapshot(byte),
            byte as i64,
            Some(format!("snapshot {byte}")),
            SnapshotTrigger::Manual,
        )
        .unwrap()
    }

    #[test]
    fn recent_is_newest_first_and_limited() {
        let temp = tempfile::tempdir().unwrap();
        let log = RefLog::open(temp.path(), "workspace").unwrap();
        for byte in 1..=5 {
            append(&log, byte);
        }

        let recent = log.recent(2).unwrap();
        assert_eq!(recent.len(), 2);
        assert_eq!(recent[0].snapshot_id, snapshot(5));
        assert_eq!(recent[1].snapshot_id, snapshot(4));
        assert_eq!(log.head().unwrap().unwrap().sequence, 5);
    }

    #[test]
    fn missing_index_is_rebuilt_from_a_legacy_log() {
        let temp = tempfile::tempdir().unwrap();
        let log = RefLog::open(temp.path(), "workspace").unwrap();
        append(&log, 1);
        append(&log, 2);
        fs::remove_file(&log.index_path).unwrap();

        assert_eq!(log.head().unwrap().unwrap().snapshot_id, snapshot(2));
        assert!(log.index_path.exists());
        assert_eq!(log.records().unwrap().len(), 2);
    }

    #[test]
    fn torn_log_and_index_tails_are_recovered() {
        let temp = tempfile::tempdir().unwrap();
        let log = RefLog::open(temp.path(), "workspace").unwrap();
        append(&log, 1);
        append(&log, 2);
        let valid_log_len = fs::metadata(&log.path).unwrap().len();
        let valid_index_len = fs::metadata(&log.index_path).unwrap().len();

        OpenOptions::new()
            .append(true)
            .open(&log.path)
            .unwrap()
            .write_all(b"AG")
            .unwrap();
        OpenOptions::new()
            .append(true)
            .open(&log.index_path)
            .unwrap()
            .write_all(b"torn")
            .unwrap();

        assert_eq!(log.head().unwrap().unwrap().snapshot_id, snapshot(2));
        assert_eq!(fs::metadata(&log.path).unwrap().len(), valid_log_len);
        assert_eq!(
            fs::metadata(&log.index_path).unwrap().len(),
            valid_index_len
        );
        assert_eq!(log.head().unwrap().unwrap().snapshot_id, snapshot(2));
    }

    #[test]
    fn complete_scan_still_detects_a_broken_hash_chain() {
        let temp = tempfile::tempdir().unwrap();
        let log = RefLog::open(temp.path(), "workspace").unwrap();
        append(&log, 1);
        append(&log, 2);

        let first = log.read_index_tail(2).unwrap()[0];
        let mut file = OpenOptions::new()
            .read(true)
            .write(true)
            .open(&log.path)
            .unwrap();
        file.seek(SeekFrom::Start(first.start + 4 + 4 + 32))
            .unwrap();
        file.write_all(b"X").unwrap();
        file.sync_data().unwrap();

        assert!(log.records().unwrap_err().to_string().contains("checksum"));
    }

    #[test]
    fn corrupt_advisory_index_record_is_rebuilt_from_the_log() {
        let temp = tempfile::tempdir().unwrap();
        let log = RefLog::open(temp.path(), "workspace").unwrap();
        for byte in 1..=5 {
            append(&log, byte);
        }

        let mut index = OpenOptions::new()
            .read(true)
            .write(true)
            .open(&log.index_path)
            .unwrap();
        index
            .seek(SeekFrom::Start(
                INDEX_MAGIC.len() as u64 + INDEX_RECORD_LEN as u64 + 17,
            ))
            .unwrap();
        index.write_all(b"X").unwrap();
        index.sync_data().unwrap();

        let recent = log.recent(4).unwrap();
        assert_eq!(
            recent
                .iter()
                .map(|record| record.snapshot_id)
                .collect::<Vec<_>>(),
            vec![snapshot(5), snapshot(4), snapshot(3), snapshot(2)]
        );
        assert_eq!(log.recent(5).unwrap().len(), 5);
    }
}
