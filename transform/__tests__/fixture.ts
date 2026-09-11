import { Utf8 } from "../../assembly/index";

@bind
class Request {
  id: u64;
  enabled: bool;
  priority: u8;
  code: i16;
  flags: u32;
  score: f32;
  ratio: f64;
  name: Utf8;

  @inline
  bump(): void {
    this.score += 10.0;
    this.flags |= 1;
  }
}

export function requestSize(): u32 {
  return offsetof<Request>();
}

export function enabledOffset(): u32 {
  return offsetof<Request>("enabled");
}

export function flagsOffset(): u32 {
  return offsetof<Request>("flags");
}

export function scoreOffset(): u32 {
  return offsetof<Request>("score");
}

export function ratioOffset(): u32 {
  return offsetof<Request>("ratio");
}

export function nameOffset(): u32 {
  return offsetof<Request>("name");
}

export function inspect(request: Request): void {
  request.bump();
}
