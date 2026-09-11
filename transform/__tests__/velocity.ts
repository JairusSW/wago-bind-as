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

export function forceAbort(): void {
  abort();
}
