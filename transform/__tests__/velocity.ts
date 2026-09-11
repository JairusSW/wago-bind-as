class Vec3 {
  x: f32;
  y: f32;
  z: f32;

  constructor(x: f32 = 0, y: f32 = 0, z: f32 = 0) {
    this.x = x;
    this.y = y;
    this.z = z;
  }
}

export function getVelocity(lastTick: Vec3, current: Vec3): Vec3 {
  return new Vec3(
    current.x - lastTick.x,
    current.y - lastTick.y,
    current.z - lastTick.z,
  );
}

export function reservedLocals(__result: Vec3, __value: Vec3): Vec3 {
  return new Vec3(__value.x - __result.x, 0, 0);
}

export function reset(value: Vec3): void {
  value.x = 0;
  value.y = 0;
  value.z = 0;
}

export function forceAbort(): void {
  abort();
}
