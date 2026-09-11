@bind
class Empty {}

export function size(): u32 {
  return offsetof<Empty>();
}
