import { Parser } from "assemblyscript/dist/assemblyscript.js";
import { Transform } from "assemblyscript/dist/transform.js";
export default class BindTransform extends Transform {
    constructor();
    afterParse(parser: Parser): void;
    private lowerClass;
}
