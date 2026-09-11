@external("host", "read_record")
declare function readRecord(offset: u32, length: u32): i32;

export function run(): i32 {
  if (memory.size() == 0) memory.grow(1);
  store<u64>(16 << 10, 0x0102030405060708);
  return readRecord(16 << 10, 8);
}
