import { createReadStream, createWriteStream } from 'node:fs';
import { readdir, stat } from 'node:fs/promises';
import { join } from 'node:path';
import { pipeline } from 'node:stream/promises';
import { createGzip } from 'node:zlib';

const block = 512;

function header(path, mode, size, type) {
  const bytes = Buffer.alloc(block);
  const octal = (value, offset, length) => {
    const encoded = value.toString(8).padStart(length - 1, '0');
    if (encoded.length >= length) {
      throw new Error('USTAR numeric field overflow');
    }
    bytes.write(encoded, offset, length - 1, 'ascii');
  };
  let name = path;
  if (Buffer.byteLength(name) > 100) {
    const split = path.lastIndexOf('/', 155);
    name = path.slice(split + 1);
    const prefix = path.slice(0, split);
    if (split < 1 || Buffer.byteLength(name) > 100 || Buffer.byteLength(prefix) > 155) {
      throw new Error('USTAR path too long: ' + path);
    }
    bytes.write(prefix, 345, 155);
  }
  bytes.write(name, 0, 100);
  octal(mode, 100, 8);
  octal(0, 108, 8);
  octal(0, 116, 8);
  octal(size, 124, 12);
  octal(0, 136, 12);
  bytes.fill(32, 148, 156);
  bytes.write(type, 156, 1);
  bytes.write('ustar\0' + '00', 257, 8, 'ascii');
  octal(
    bytes.reduce((sum, byte) => sum + byte, 0),
    148,
    7,
  );
  return bytes;
}

async function* entries(parent, name, relative = '') {
  const diskPath = join(parent, name);
  const archivePath = relative ? relative + '/' + name : name;
  yield header(archivePath, 0o755, 0, '5');
  for (const item of (await readdir(diskPath, { withFileTypes: true })).sort((a, b) =>
    a.name < b.name ? -1 : Number(a.name > b.name),
  )) {
    if (item.isDirectory()) {
      yield* entries(diskPath, item.name, archivePath);
      continue;
    }
    if (!item.isFile()) {
      throw new Error('Release entry must be a regular file: ' + item.name);
    }
    const file = join(diskPath, item.name);
    const size = (await stat(file)).size;
    const executable =
      (!relative && /^(blog|publisher)$/.test(item.name)) ||
      (name === 'scripts' && /^(backup-daily|backup-retain)\.sh$/.test(item.name));
    yield header(archivePath + '/' + item.name, executable ? 0o755 : 0o644, size, '0');
    yield* createReadStream(file);
    if (size % block) {
      yield Buffer.alloc(block - (size % block));
    }
  }
}

export async function writeTarGzip(parent, version, output) {
  async function* archive() {
    yield* entries(parent, version);
    yield Buffer.alloc(2 * block);
  }
  await pipeline(archive(), createGzip({ level: 9 }), createWriteStream(output, { flags: 'wx' }));
}
