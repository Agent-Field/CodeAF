use std::io::{self, Read};

pub const SMALL_FILE_LIMIT: usize = 64 * 1024;
pub const MIN_CHUNK: usize = 64 * 1024;
pub const AVG_CHUNK: usize = 256 * 1024;
pub const MAX_CHUNK: usize = 1024 * 1024;
const READ_BUFFER: usize = 64 * 1024;

// FastCDC-inspired streaming gear hash. It retains bounded memory and stable
// content-defined boundaries without requiring a full file-sized allocation.
pub struct ChunkStream<R> {
    reader: R,
    buffer: Vec<u8>,
    position: usize,
    filled: usize,
    eof: bool,
}

impl<R: Read> ChunkStream<R> {
    pub fn new(reader: R) -> Self {
        Self {
            reader,
            buffer: vec![0; READ_BUFFER],
            position: 0,
            filled: 0,
            eof: false,
        }
    }

    pub fn next_chunk(&mut self) -> io::Result<Option<Vec<u8>>> {
        if self.eof {
            return Ok(None);
        }

        let mut out = Vec::with_capacity(AVG_CHUNK);
        let mut hash = 0_u64;
        let early_mask = (AVG_CHUNK as u64 * 2) - 1;
        let late_mask = (AVG_CHUNK as u64 / 2) - 1;
        while out.len() < MAX_CHUNK {
            if self.position == self.filled {
                self.filled = self.reader.read(&mut self.buffer)?;
                self.position = 0;
                if self.filled == 0 {
                    self.eof = true;
                    break;
                }
            }

            let available = self.filled - self.position;
            let take = available.min(MAX_CHUNK - out.len());
            let start = self.position;
            for offset in 0..take {
                let value = self.buffer[start + offset];
                out.push(value);
                hash = hash.rotate_left(1).wrapping_add(GEAR[value as usize]);

                let len = out.len();
                let boundary = if len < MIN_CHUNK {
                    false
                } else if len < AVG_CHUNK {
                    hash & early_mask == 0
                } else {
                    hash & late_mask == 0
                };
                if boundary {
                    self.position += offset + 1;
                    return Ok(Some(out));
                }
            }
            self.position += take;
        }

        if out.is_empty() {
            Ok(None)
        } else {
            Ok(Some(out))
        }
    }
}

const fn gear_table() -> [u64; 256] {
    let mut table = [0_u64; 256];
    let mut i = 0;
    let mut state = 0x9e3779b97f4a7c15_u64;
    while i < 256 {
        state ^= state >> 12;
        state ^= state << 25;
        state ^= state >> 27;
        table[i] = state.wrapping_mul(0x2545f4914f6cdd1d);
        i += 1;
    }
    table
}

const GEAR: [u64; 256] = gear_table();

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Cursor;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::Arc;

    #[test]
    fn chunks_are_bounded_and_reconstruct_input() {
        let input: Vec<u8> = (0..(MAX_CHUNK * 3 + 17)).map(|n| (n % 251) as u8).collect();
        let mut stream = ChunkStream::new(Cursor::new(&input));
        let mut rebuilt = Vec::new();
        let mut count = 0;
        while let Some(chunk) = stream.next_chunk().unwrap() {
            assert!(chunk.len() <= MAX_CHUNK);
            rebuilt.extend_from_slice(&chunk);
            count += 1;
        }
        assert!(count >= 3);
        assert_eq!(rebuilt, input);
    }

    struct CountedReader {
        cursor: Cursor<Vec<u8>>,
        reads: Arc<AtomicUsize>,
    }

    impl Read for CountedReader {
        fn read(&mut self, buffer: &mut [u8]) -> io::Result<usize> {
            self.reads.fetch_add(1, Ordering::Relaxed);
            self.cursor.read(buffer)
        }
    }

    #[test]
    fn reads_large_inputs_in_blocks() {
        let reads = Arc::new(AtomicUsize::new(0));
        let reader = CountedReader {
            cursor: Cursor::new(vec![3; MAX_CHUNK * 3]),
            reads: Arc::clone(&reads),
        };
        let mut stream = ChunkStream::new(reader);
        let mut length = 0;
        while let Some(chunk) = stream.next_chunk().unwrap() {
            length += chunk.len();
        }
        assert_eq!(length, MAX_CHUNK * 3);
        assert!(reads.load(Ordering::Relaxed) < 100);
    }
}
