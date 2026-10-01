//! Stage 1 contract 5.2 and 5.4: `export` and `published`.

mod common;

use common::*;
use furrow::exchange::export::{published, DEFAULT_MAX_FRAME};
use furrow::exchange::keys::{keys_from, CELL_KEY_VAR, DEDUP_VAR};
use furrow::exchange::ledger::Ledger;
use furrow::exchange::open_store;
use furrow::exchange::survey::survey;
use furrow::model::SnapshotTrigger;
use furrow::model::{id_hex, ObjectKind};
use furrow::repository::{FurrowRepository, SealOptions};
use furrow::sealer::Sealer;
use sha2::{Digest, Sha256};
use std::collections::HashSet;
use std::fs;

const SMALL_FRAME: usize = 300 * 1024;

/// A second, independent reading of the frame layout in the contract, so the
/// encoder is not judged only by its own decoder.
fn strict_decode(frame: &[u8]) -> (serde_json::Value, Vec<(String, Vec<u8>)>) {
    assert_eq!(&frame[..5], b"AGEF\x01");
    let header_len = u32::from_le_bytes(frame[5..9].try_into().unwrap()) as usize;
    let header: serde_json::Value = serde_json::from_slice(&frame[9..9 + header_len]).unwrap();
    let payload = &frame[9 + header_len..];
    let mut offset = 0_u64;
    let mut seen = HashSet::new();
    let mut objects = Vec::new();
    for entry in header["objects"].as_array().unwrap() {
        assert_eq!(
            entry["off"].as_u64().unwrap(),
            offset,
            "offsets are contiguous"
        );
        let len = entry["len"].as_u64().unwrap();
        let rid = entry["rid"].as_str().unwrap().to_owned();
        assert_eq!(rid.len(), 64);
        assert!(rid
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b)));
        assert!(seen.insert(rid.clone()), "no rid repeats");
        let bytes = payload[offset as usize..(offset + len) as usize].to_vec();
        assert!(bytes.starts_with(b"AGEO\x01"));
        offset += len;
        objects.push((rid, bytes));
    }
    assert_eq!(
        offset as usize,
        payload.len(),
        "objects cover the payload exactly"
    );
    (header, objects)
}

#[test]
fn export_frames_decode() {
    let src = source();
    let outbox = src._temp.path().join("outbox");
    let report = export(&src, LEDGER, &outbox, SMALL_FRAME);
    assert!(report.frames.len() > 3, "a 3 MiB file needs several frames");
    let key_id = hex::encode(&Sha256::digest([0x11; 32])[..16]);
    let mut total = 0;
    for info in &report.frames {
        assert!(info.path.is_absolute());
        let bytes = fs::read(&info.path).unwrap();
        assert_eq!(bytes.len(), info.bytes);
        let (header, objects) = strict_decode(&bytes);
        assert_eq!(header["V"], 1);
        assert_eq!(header["cell_key_id"], key_id);
        assert_eq!(objects.len(), info.objects);
        assert!(
            bytes.len() <= SMALL_FRAME || objects.len() == 1,
            "only a lone object may exceed the bound"
        );
        total += objects.len();
    }
    assert_eq!(total, report.objects);
    let survey = survey(&open_store(&src.data).unwrap(), src.head, 1).unwrap();
    assert_eq!(
        total,
        survey.present.len(),
        "every reachable object is sent once"
    );
    assert_eq!(
        report.head_rid,
        id_hex(&sealer().remote_id(ObjectKind::Snapshot, &src.head))
    );
}

#[test]
fn export_order_is_chunks_blobs_xattrs_trees_snapshot_last() {
    let src = source();
    let report = export(&src, LEDGER, &src._temp.path().join("outbox"), SMALL_FRAME);
    let store = open_store(&src.data).unwrap();
    let kind_of: std::collections::HashMap<_, _> = survey(&store, src.head, 1)
        .unwrap()
        .present
        .into_iter()
        .map(|(kind, id)| (sealer().remote_id(kind, &id), kind))
        .collect();
    let ranks: Vec<usize> = objects_of(&frame_paths(&report))
        .iter()
        .map(|(rid, _)| match kind_of[rid] {
            ObjectKind::Chunk => 0,
            ObjectKind::Blob => 1,
            ObjectKind::Xattrs => 2,
            ObjectKind::Tree => 3,
            ObjectKind::Snapshot => 4,
        })
        .collect();
    assert!(ranks.windows(2).all(|pair| pair[0] <= pair[1]), "{ranks:?}");
    assert_eq!(ranks.iter().filter(|rank| **rank == 4).count(), 1);
    assert_eq!(*ranks.last().unwrap(), 4, "the snapshot is the last object");
}

#[test]
fn an_object_larger_than_max_frame_gets_a_frame_of_its_own() {
    let src = source();
    let report = export(&src, LEDGER, &src._temp.path().join("outbox"), 1024);
    assert!(report
        .frames
        .iter()
        .any(|info| info.bytes > 1024 && info.objects == 1));
    assert!(report
        .frames
        .iter()
        .all(|info| info.bytes <= 1024 || info.objects == 1));
}

#[test]
fn second_export_is_empty() {
    let src = source();
    let outbox = src._temp.path().join("outbox");
    let first = export(&src, LEDGER, &outbox, SMALL_FRAME);
    let ledger = Ledger::named(&src.data, LEDGER).unwrap();
    assert_eq!(
        published(&ledger, &frame_paths(&first)).unwrap(),
        first.objects
    );
    let second = export(&src, LEDGER, &outbox, SMALL_FRAME);
    assert!(second.frames.is_empty());
    assert_eq!((second.objects, second.bytes), (0, 0));
    assert_eq!(second.head_rid, first.head_rid);
}

#[test]
fn published_appends_header_rids_and_deletes_frame_files() {
    let src = source();
    let report = export(&src, LEDGER, &src._temp.path().join("outbox"), SMALL_FRAME);
    let paths = frame_paths(&report);
    let expected: HashSet<_> = objects_of(&paths).into_iter().map(|(rid, _)| rid).collect();
    let ledger = Ledger::named(&src.data, LEDGER).unwrap();
    published(&ledger, &paths).unwrap();
    assert!(paths.iter().all(|path| !path.exists()));
    assert_eq!(ledger.recorded().unwrap(), expected);
    let text = fs::read_to_string(src.data.join(format!("published.{LEDGER}"))).unwrap();
    assert_eq!(text.lines().count(), expected.len(), "one rid per line");
    // A retry after a crash between the two steps finds the files gone.
    assert_eq!(published(&ledger, &paths).unwrap(), 0);
}

#[test]
fn published_refuses_a_file_that_is_not_a_frame() {
    let src = source();
    let stray = src._temp.path().join("stray");
    fs::write(&stray, b"not a frame").unwrap();
    let ledger = Ledger::named(&src.data, LEDGER).unwrap();
    assert!(published(&ledger, std::slice::from_ref(&stray)).is_err());
    assert!(stray.exists());
    assert!(ledger.recorded().unwrap().is_empty());
}

#[test]
fn reexport_after_crash_is_identical() {
    let src = source();
    let outbox = src._temp.path().join("outbox");
    let first = export(&src, LEDGER, &outbox, SMALL_FRAME);
    let before: Vec<_> = first
        .frames
        .iter()
        .map(|f| (f.path.clone(), fs::read(&f.path).unwrap()))
        .collect();
    // The upload happened and the process died before `published`.
    let again = export(&src, LEDGER, &outbox, SMALL_FRAME);
    let after: Vec<_> = again
        .frames
        .iter()
        .map(|f| (f.path.clone(), fs::read(&f.path).unwrap()))
        .collect();
    assert_eq!(before, after);
    assert_eq!(first.head_rid, again.head_rid);
}

#[test]
fn a_crash_after_some_frames_were_published_resends_only_the_rest() {
    let src = source();
    let outbox = src._temp.path().join("outbox");
    let first = export(&src, LEDGER, &outbox, SMALL_FRAME);
    let mut unsent: Vec<_> = objects_of(&frame_paths(&first))
        .into_iter()
        .map(|(rid, _)| rid)
        .collect();
    unsent.drain(..first.frames[0].objects);
    let ledger = Ledger::named(&src.data, LEDGER).unwrap();
    published(&ledger, &frame_paths(&first)[..1]).unwrap();
    let again = export(&src, LEDGER, &outbox, SMALL_FRAME);
    let resent: Vec<_> = objects_of(&frame_paths(&again))
        .into_iter()
        .map(|(rid, _)| rid)
        .collect();
    assert_eq!(unsent, resent);
}

#[test]
fn new_ledger_resends_everything() {
    let src = source();
    let outbox = src._temp.path().join("outbox");
    let first = export(&src, LEDGER, &outbox, SMALL_FRAME);
    let ledger = Ledger::named(&src.data, LEDGER).unwrap();
    published(&ledger, &frame_paths(&first)).unwrap();
    assert!(export(&src, LEDGER, &outbox, SMALL_FRAME).frames.is_empty());

    // Another store means another ledger name, which starts empty.
    let other = export(&src, OTHER_LEDGER, &outbox, SMALL_FRAME);
    assert_eq!(other.objects, first.objects);
    assert_eq!(
        objects_of(&frame_paths(&other)),
        objects_of(&frame_paths(&first))
    );
}

#[test]
fn a_ledger_name_must_be_sixteen_lowercase_hex_characters() {
    let dir = tempfile::tempdir().unwrap();
    for bad in [
        "",
        "0123456789abcde",
        "0123456789abcdef0",
        "0123456789ABCDEF",
        "../../etc/passwd!",
        "0123456789abcdeg",
    ] {
        assert!(Ledger::named(dir.path(), bad).is_err(), "{bad:?}");
    }
    assert!(Ledger::named(dir.path(), LEDGER).is_ok());
}

#[test]
fn head_that_is_not_in_the_store_is_refused() {
    let src = source();
    let error = export_with(
        &src.data,
        [9; 32],
        &keys(),
        LEDGER,
        &src._temp.path().join("o"),
        SMALL_FRAME,
    )
    .err()
    .unwrap();
    assert!(error.to_string().contains("not complete"), "{error:#}");
}

#[test]
fn keys_never_on_argv_or_disk() {
    let cell_hex = hex::encode([0x11; 32]);
    let dedup_hex = hex::encode([0x22; 32]);

    // The only way in is the two named variables.
    let env = |name: &str| match name {
        CELL_KEY_VAR => Some(cell_hex.clone()),
        DEDUP_VAR => Some(dedup_hex.clone()),
        _ => None,
    };
    let parsed = keys_from(env).unwrap();
    assert_eq!((parsed.cell_key, parsed.dedup), ([0x11; 32], [0x22; 32]));
    for broken in [None, Some("zz".to_owned()), Some("11".repeat(31))] {
        let lookup = |name: &str| {
            (name == CELL_KEY_VAR)
                .then(|| broken.clone())
                .flatten()
                .or_else(|| (name == DEDUP_VAR).then(|| dedup_hex.clone()))
        };
        let message = keys_from(lookup).err().unwrap().to_string();
        assert!(
            message.contains(CELL_KEY_VAR) && !message.contains("zz"),
            "{message}"
        );
    }

    // Nothing the engine writes holds either secret, raw or in hex.
    let src = source();
    let outbox = src._temp.path().join("outbox");
    let report = export(&src, LEDGER, &outbox, SMALL_FRAME);
    published(
        &Ledger::named(&src.data, LEDGER).unwrap(),
        &frame_paths(&report)[..1],
    )
    .unwrap();
    export(&src, OTHER_LEDGER, &outbox, SMALL_FRAME);
    let needles: [Vec<u8>; 4] = [
        vec![0x11; 32],
        vec![0x22; 32],
        cell_hex.into_bytes(),
        dedup_hex.into_bytes(),
    ];
    let files: Vec<_> = all_files(&src.data)
        .into_iter()
        .chain(all_files(&outbox))
        .collect();
    assert!(files.len() > 3);
    for path in files {
        let bytes = fs::read(&path).unwrap();
        for needle in &needles {
            assert!(
                !bytes.windows(needle.len()).any(|w| w == needle.as_slice()),
                "{}",
                path.display()
            );
        }
    }
}

/// The default frame size is the contract's `BulkFrame` (§22.6), not the old
/// 1 MiB: a bulk take fetches whole megabyte-scale objects in one frame. The
/// fixture's 3 MiB blob lands in a frame the old default would have split.
#[test]
fn the_default_frame_size_is_bulk_frame() {
    assert_eq!(DEFAULT_MAX_FRAME, 8 << 20);
    let src = source();
    let outbox = src._temp.path().join("outbox");
    let report = export(&src, LEDGER, &outbox, DEFAULT_MAX_FRAME);
    assert!(
        report.frames.iter().any(|info| info.bytes > 1 << 20),
        "no frame needed the bigger default"
    );
    assert!(
        report
            .frames
            .iter()
            .all(|info| info.bytes <= DEFAULT_MAX_FRAME),
        "a frame outgrew the default"
    );

    // A save smaller than `BulkFrame` still writes one small frame: a small
    // payload in a fresh store closes its frame as soon as it ends, not at
    // the default.
    let temp = tempfile::tempdir().unwrap();
    let tree = temp.path().join("tree-s");
    fs::create_dir_all(&tree).unwrap();
    fs::write(tree.join("note.txt"), b"small\n").unwrap();
    let (_, head) = FurrowRepository::attach_and_seal_in(
        &temp.path().join("data-s"),
        &tree,
        None,
        SnapshotTrigger::Manual,
        SealOptions::default(),
    )
    .unwrap();
    let small = export_with(
        &temp.path().join("data-s"),
        head,
        &keys(),
        LEDGER,
        &outbox,
        64 * 1024,
    )
    .unwrap();
    assert_eq!(small.frames.len(), 1);
    assert!(small.frames[0].bytes <= 64 * 1024);
}
