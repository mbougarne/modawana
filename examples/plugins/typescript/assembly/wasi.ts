@external("wasi_snapshot_preview1", "fd_read")
declare function fdRead(fd: i32, vectors: usize, count: i32, read: usize): i32;

@external("wasi_snapshot_preview1", "fd_write")
declare function fdWrite(fd: i32, vectors: usize, count: i32, written: usize): i32;

@external("wasi_snapshot_preview1", "proc_exit")
declare function procExit(code: i32): void;

const ioVector = memory.data(8, 4);
const transferred = memory.data(4, 4);

export function abort(_message: string | null, _file: string | null, _line: u32, _column: u32): void {
  procExit(1);
}

export function readInput(limit: i32): string {
  // Reserve one extra byte to distinguish an exact-size envelope from overflow.
  const bytes = new Uint8Array(limit + 1);
  let offset = 0;
  while (offset < bytes.length) {
    store<u32>(ioVector, <u32>(bytes.dataStart + offset));
    store<u32>(ioVector + 4, <u32>(bytes.length - offset));
    if (fdRead(0, ioVector, 1, transferred) != 0) {
      procExit(1);
    }
    const length = load<i32>(transferred);
    if (length == 0) {
      return String.UTF8.decodeUnsafe(bytes.dataStart, offset);
    }
    offset += length;
  }
  procExit(1);
  return "";
}

export function writeText(text: string): void {
  const bytes = Uint8Array.wrap(String.UTF8.encode(text));
  let offset = 0;
  while (offset < bytes.length) {
    store<u32>(ioVector, <u32>(bytes.dataStart + offset));
    store<u32>(ioVector + 4, <u32>(bytes.length - offset));
    if (fdWrite(1, ioVector, 1, transferred) != 0) {
      procExit(1);
    }
    const length = load<i32>(transferred);
    if (length == 0) {
      procExit(1);
    }
    offset += length;
  }
}
