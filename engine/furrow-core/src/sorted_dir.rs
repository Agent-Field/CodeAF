//! Bytewise directory ordering with bounded memory for very large directories.

use std::cmp::Ordering;
use std::collections::BinaryHeap;
use std::ffi::OsString;
use std::fs::File;
use std::io::{self, BufRead, BufReader, BufWriter, Read, Seek, SeekFrom, Write};
use std::os::unix::ffi::{OsStrExt, OsStringExt};
use std::path::Path;

const NAMES_PER_RUN: usize = 8 * 1024;

pub struct SortedDirectory {
    inner: SortedDirectoryInner,
}

enum SortedDirectoryInner {
    Memory(std::vec::IntoIter<OsString>),
    Merge(MergeRuns),
}

struct MergeRuns {
    runs: Vec<BufReader<File>>,
    heap: BinaryHeap<HeapName>,
}

#[derive(Eq, PartialEq)]
struct HeapName {
    name: Vec<u8>,
    run: usize,
}

impl Ord for HeapName {
    fn cmp(&self, other: &Self) -> Ordering {
        other
            .name
            .cmp(&self.name)
            .then_with(|| other.run.cmp(&self.run))
    }
}

impl PartialOrd for HeapName {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl SortedDirectory {
    pub fn open(path: &Path) -> anyhow::Result<Self> {
        let mut names = Vec::with_capacity(NAMES_PER_RUN);
        let mut runs = Vec::new();
        for entry in std::fs::read_dir(path)? {
            names.push(entry?.file_name());
            if names.len() == NAMES_PER_RUN {
                runs.push(spill_run(&mut names)?);
            }
        }

        if runs.is_empty() {
            names.sort_unstable_by(|left, right| left.as_bytes().cmp(right.as_bytes()));
            return Ok(Self {
                inner: SortedDirectoryInner::Memory(names.into_iter()),
            });
        }
        if !names.is_empty() {
            runs.push(spill_run(&mut names)?);
        }

        let mut readers: Vec<_> = runs.into_iter().map(BufReader::new).collect();
        let mut heap = BinaryHeap::new();
        for (run, reader) in readers.iter_mut().enumerate() {
            if let Some(name) = read_name(reader)? {
                heap.push(HeapName { name, run });
            }
        }
        Ok(Self {
            inner: SortedDirectoryInner::Merge(MergeRuns {
                runs: readers,
                heap,
            }),
        })
    }
}

impl Iterator for SortedDirectory {
    type Item = io::Result<OsString>;

    fn next(&mut self) -> Option<Self::Item> {
        match &mut self.inner {
            SortedDirectoryInner::Memory(names) => names.next().map(Ok),
            SortedDirectoryInner::Merge(merge) => {
                let item = merge.heap.pop()?;
                match read_name(&mut merge.runs[item.run]) {
                    Ok(Some(name)) => merge.heap.push(HeapName {
                        name,
                        run: item.run,
                    }),
                    Ok(None) => {}
                    Err(error) => return Some(Err(error)),
                }
                Some(Ok(OsString::from_vec(item.name)))
            }
        }
    }
}

fn spill_run(names: &mut Vec<OsString>) -> io::Result<File> {
    names.sort_unstable_by(|left, right| left.as_bytes().cmp(right.as_bytes()));
    let mut writer = BufWriter::new(tempfile::tempfile()?);
    for name in names.drain(..) {
        let bytes = name.as_bytes();
        let length = u32::try_from(bytes.len())
            .map_err(|_| io::Error::new(io::ErrorKind::InvalidData, "filename too long"))?;
        writer.write_all(&length.to_le_bytes())?;
        writer.write_all(bytes)?;
    }
    writer.flush()?;
    let mut file = writer
        .into_inner()
        .map_err(|error| io::Error::new(error.error().kind(), error.to_string()))?;
    file.seek(SeekFrom::Start(0))?;
    Ok(file)
}

fn read_name(reader: &mut BufReader<File>) -> io::Result<Option<Vec<u8>>> {
    if reader.fill_buf()?.is_empty() {
        return Ok(None);
    }
    let mut length = [0_u8; 4];
    reader.read_exact(&mut length)?;
    let length = u32::from_le_bytes(length) as usize;
    let mut name = vec![0_u8; length];
    reader.read_exact(&mut name)?;
    Ok(Some(name))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn spills_and_merges_large_directories_in_byte_order() {
        let temporary = tempfile::tempdir().unwrap();
        for index in (0..20_000).rev() {
            File::create(temporary.path().join(format!("entry-{index:08}"))).unwrap();
        }
        let names: Vec<_> = SortedDirectory::open(temporary.path())
            .unwrap()
            .collect::<Result<_, _>>()
            .unwrap();
        assert_eq!(names.len(), 20_000);
        assert!(names.windows(2).all(|pair| pair[0] < pair[1]));
    }
}
