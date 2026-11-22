import { readInput, writeText } from './wasi';

export function _start(): void {
  // This example has one entry point. Echo its host-validated MCP envelope.
  writeText(readInput(24 * 1024));
}
