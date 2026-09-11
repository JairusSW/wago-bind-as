import { bytesStartsWith, validSpan } from "../../assembly/index";
import { Request } from "./bindings_gen";

const ADMIN: StaticArray<u8> = [97, 100, 109, 105, 110];

// A generated transform will make this property-shaped in the final surface.
// This explicit form is the allocation-free floor that ergonomic lowering must
// continue to match instruction-for-instruction.
export function inspect(
  regionBase: u32,
  regionLength: u32,
  requestOffset: u32,
): i32 {
  if (!Request.valid(regionLength, requestOffset)) return -1;
  const name = Request.name(regionBase, requestOffset);
  if (!validSpan(regionLength, name)) return -2;
  if (
    bytesStartsWith(regionBase, regionLength, name, changetype<usize>(ADMIN), 5)
  ) {
    Request.setScore(
      regionBase,
      requestOffset,
      Request.score(regionBase, requestOffset) + 10.0,
    );
  }
  return 0;
}
