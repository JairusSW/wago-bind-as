// Exact32 primitives used by generated bindings. These functions allocate
// nothing and keep UTF-8 in its wire representation.

export type Utf8 = u64;
export type Bytes = u64;
export type TimestampNanos = i64;
export type DurationNanos = i64;

@inline
export function span(offset: u32, length: u32): u64 {
  return <u64>offset | (<u64>length << 32);
}

@inline
export function spanOffset(value: u64): u32 {
  return <u32>value;
}

@inline
export function spanLength(value: u64): u32 {
  return <u32>(value >> 32);
}

@inline
export function validRange(regionLength: u32, offset: u32, length: u32): bool {
  return offset <= regionLength && length <= regionLength - offset;
}

@inline
export function validSpan(regionLength: u32, value: u64): bool {
  return validRange(regionLength, spanOffset(value), spanLength(value));
}

@inline
export function vectorOffset(base: usize, descriptor: usize): u32 {
  return load<u32>(base + descriptor);
}

@inline
export function vectorLength(base: usize, descriptor: usize): u32 {
  return load<u32>(base + descriptor + 4);
}

@inline
export function vectorCapacity(base: usize, descriptor: usize): u32 {
  return load<u32>(base + descriptor + 8);
}

export function validVector(
  base: usize,
  regionLength: u32,
  descriptor: usize,
  stride: u32,
  alignment: u32,
): bool {
  if (stride == 0 || alignment == 0 || (alignment & (alignment - 1)) != 0)
    return false;
  if (!validRange(regionLength, <u32>descriptor, 12)) return false;
  const offset = vectorOffset(base, descriptor);
  const length = vectorLength(base, descriptor);
  const capacity = vectorCapacity(base, descriptor);
  if (length > capacity || (offset & (alignment - 1)) != 0) return false;
  const bytes = <u64>capacity * stride;
  return bytes <= u32.MAX_VALUE && validRange(regionLength, offset, <u32>bytes);
}

export function bytesEqual(
  base: usize,
  regionLength: u32,
  value: u64,
  expected: usize,
  expectedLength: u32,
): bool {
  const offset = spanOffset(value);
  const length = spanLength(value);
  if (length != expectedLength || !validRange(regionLength, offset, length))
    return false;
  for (let i: u32 = 0; i < length; i++) {
    if (load<u8>(base + offset + i) != load<u8>(expected + i)) return false;
  }
  return true;
}

export function bytesStartsWith(
  base: usize,
  regionLength: u32,
  value: u64,
  prefix: usize,
  prefixLength: u32,
): bool {
  const offset = spanOffset(value);
  const length = spanLength(value);
  if (prefixLength > length || !validRange(regionLength, offset, length))
    return false;
  for (let i: u32 = 0; i < prefixLength; i++) {
    if (load<u8>(base + offset + i) != load<u8>(prefix + i)) return false;
  }
  return true;
}

export function bytesEqualIgnoreCaseASCII(
  base: usize,
  regionLength: u32,
  value: u64,
  expected: usize,
  expectedLength: u32,
): bool {
  const offset = spanOffset(value);
  const length = spanLength(value);
  if (length != expectedLength || !validRange(regionLength, offset, length))
    return false;
  for (let i: u32 = 0; i < length; i++) {
    let a = load<u8>(base + offset + i);
    let b = load<u8>(expected + i);
    if (a >= 65 && a <= 90) a |= 32;
    if (b >= 65 && b <= 90) b |= 32;
    if (a != b) return false;
  }
  return true;
}

export function bytesEndsWith(
  base: usize,
  regionLength: u32,
  value: u64,
  suffix: usize,
  suffixLength: u32,
): bool {
  const offset = spanOffset(value);
  const length = spanLength(value);
  if (suffixLength > length || !validRange(regionLength, offset, length)) return false;
  const start = offset + length - suffixLength;
  for (let i: u32 = 0; i < suffixLength; i++) {
    if (load<u8>(base + start + i) != load<u8>(suffix + i)) return false;
  }
  return true;
}

export function bytesIndexOfByte(
  base: usize,
  regionLength: u32,
  value: u64,
  needle: u8,
): i32 {
  const offset = spanOffset(value);
  const length = spanLength(value);
  if (!validRange(regionLength, offset, length)) return -1;
  for (let i: u32 = 0; i < length; i++) {
    if (load<u8>(base + offset + i) == needle) return <i32>i;
  }
  return -1;
}

export function bytesFNV1a32(base: usize, regionLength: u32, value: u64): u32 {
  const offset = spanOffset(value);
  const length = spanLength(value);
  if (!validRange(regionLength, offset, length)) return 0;
  let hash: u32 = 2166136261;
  for (let i: u32 = 0; i < length; i++) {
    hash ^= load<u8>(base + offset + i);
    hash *= 16777619;
  }
  return hash;
}

// Returns u64.MAX_VALUE when the requested byte range is invalid.
export function sliceBytes(
  regionLength: u32,
  value: u64,
  start: u32,
  end: u32,
): u64 {
  const length = spanLength(value);
  if (
    !validRange(regionLength, spanOffset(value), length) ||
    start > end ||
    end > length
  )
    return u64.MAX_VALUE;
  return span(spanOffset(value) + start, end - start);
}

@inline
function continuation(value: u8): bool {
  return (value & 0xc0) == 0x80;
}

export function validUtf8(base: usize, regionLength: u32, value: u64): bool {
  const offset = spanOffset(value);
  const length = spanLength(value);
  if (!validRange(regionLength, offset, length)) return false;
  let i: u32 = 0;
  while (i < length) {
    const a = load<u8>(base + offset + i++);
    if (a < 0x80) continue;
    if (a < 0xc2 || a > 0xf4) return false;
    if (a < 0xe0) {
      if (i >= length || !continuation(load<u8>(base + offset + i++))) return false;
      continue;
    }
    if (i + 1 >= length) return false;
    const b = load<u8>(base + offset + i++);
    const c = load<u8>(base + offset + i++);
    if (!continuation(b) || !continuation(c)) return false;
    if ((a == 0xe0 && b < 0xa0) || (a == 0xed && b >= 0xa0)) return false;
    if (a < 0xf0) continue;
    if (i >= length) return false;
    const d = load<u8>(base + offset + i++);
    if (!continuation(d)) return false;
    if ((a == 0xf0 && b < 0x90) || (a == 0xf4 && b >= 0x90)) return false;
  }
  return true;
}

// Returns u32.MAX_VALUE for an invalid descriptor or index.
export function vectorAt(
  base: usize,
  regionLength: u32,
  descriptor: usize,
  stride: u32,
  alignment: u32,
  index: u32,
): u32 {
  if (!validVector(base, regionLength, descriptor, stride, alignment)) return u32.MAX_VALUE;
  const length = vectorLength(base, descriptor);
  if (index >= length) return u32.MAX_VALUE;
  return vectorOffset(base, descriptor) + index * stride;
}

// Appends one zeroed element within existing capacity and returns its offset.
// Growth remains arena-owner controlled and returns u32.MAX_VALUE here.
export function vectorAppend(
  base: usize,
  regionLength: u32,
  descriptor: usize,
  stride: u32,
  alignment: u32,
): u32 {
  if (!validVector(base, regionLength, descriptor, stride, alignment)) return u32.MAX_VALUE;
  const length = vectorLength(base, descriptor);
  if (length >= vectorCapacity(base, descriptor)) return u32.MAX_VALUE;
  const offset = vectorOffset(base, descriptor) + length * stride;
  memory.fill(base + offset, 0, stride);
  store<u32>(base + descriptor + 4, length + 1);
  return offset;
}
