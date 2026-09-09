// Bytes as a hex dump: offset, sixteen hex columns, the printable ASCII.
// One renderer for the body view and the gRPC frame view.

export function hexdump(bytes: Uint8Array): string[] {
  const out: string[] = []
  for (let off = 0; off < bytes.length; off += 16) {
    const row = bytes.subarray(off, off + 16)
    const hex = Array.from(row, (b) => b.toString(16).padStart(2, '0'))
    const ascii = Array.from(row, (b) => (b >= 0x20 && b < 0x7f ? String.fromCharCode(b) : '.'))
    out.push(
      off.toString(16).padStart(8, '0') +
        '  ' +
        hex.slice(0, 8).join(' ').padEnd(23) +
        '  ' +
        hex.slice(8).join(' ').padEnd(23) +
        '  |' +
        ascii.join('') +
        '|',
    )
  }
  return out
}

// Bytes out of a base64 string, the way a JSON []byte arrives.
export function fromBase64(b64: string): Uint8Array {
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}
