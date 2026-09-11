import {
  ClassDeclaration,
  BlockStatement,
  CommonFlags,
  DecoratorNode,
  FieldDeclaration,
  FunctionDeclaration,
  IdentifierExpression,
  NamedTypeNode,
  NewExpression,
  Node,
  NodeKind,
  ParameterKind,
  Parser,
  ReturnStatement,
  Source,
  Statement,
  TypeName,
  TypeNode,
  addGlobalAlias,
} from "assemblyscript/dist/assemblyscript.js";
import { Transform } from "assemblyscript/dist/transform.js";
import { spawnSync } from "node:child_process";
import {
  readFileSync,
  mkdirSync,
  readdirSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

type WireField = { name: string; type: string };
type WireType = { name: string; fields: WireField[] };
type WireParameter = { name: string; type: string };
type WireFunction = {
  name: string;
  parameters: WireParameter[];
  result?: string;
  owns_result?: boolean;
};

const scalarTypes = new Set([
  "bool",
  "i8",
  "u8",
  "i16",
  "u16",
  "i32",
  "u32",
  "i64",
  "u64",
  "f32",
  "f64",
  "Utf8",
  "Bytes",
  "TimestampNanos",
  "DurationNanos",
]);

export default class BindTransform extends Transform {
  constructor() {
    super();
    addGlobalAlias(this.program.options, "abort", "__wbas_abort");
    this.program.parser.parseFile(abortSource(), "~lib/__wbas_abort.ts", false);
    const projectRoot = path.resolve(
      process.cwd(),
      process.env.WAGO_BIND_SYNC_ROOT || ".",
    );
    if (hasGoBindDirective(projectRoot)) runGoSync(projectRoot);
  }

  afterParse(parser: Parser): void {
    const classes = new Map<string, ClassDeclaration>();
    const declarations: FunctionDeclaration[] = [];
    for (const source of parser.sources) {
      if (source.isLibrary) continue;
      collectDeclarations(source.statements, classes, declarations);
    }

    const functions: WireFunction[] = [];
    const boundaryTypes = new Set<string>();
    for (const declaration of declarations) {
      if (!declaration.is(CommonFlags.Export)) continue;
      if (declaration.name.text.startsWith("__wbas_"))
        throw new Error(
          `exported binding ${declaration.name.text} uses the reserved __wbas_ prefix`,
        );
      if (declaration.typeParameters?.length)
        throw new Error(
          `exported binding ${declaration.name.text} cannot be generic`,
        );
      const parameters: WireParameter[] = [];
      let isBinding = false;
      for (const parameter of declaration.signature.parameters) {
        if (parameter.parameterKind != ParameterKind.Default)
          throw new Error(
            `exported binding ${declaration.name.text} cannot use optional or rest parameters`,
          );
        const type = printType(parameter.type);
        const record = classes.get(type);
        if (record) {
          boundaryTypes.add(type);
          if (!hasDecorator(record.decorators, "bind")) isBinding = true;
        }
        parameters.push({ name: parameter.name.text, type: schemaType(type) });
      }
      const writtenResult = printType(declaration.signature.returnType);
      const resultRecord = classes.get(writtenResult);
      if (resultRecord) {
        boundaryTypes.add(writtenResult);
        if (!hasDecorator(resultRecord.decorators, "bind")) isBinding = true;
      }
      if (!isBinding) continue;
      for (const parameter of parameters) {
        if (!classes.has(parameter.type))
          throw new Error(
            `binding ${declaration.name.text} mixes record and non-record parameters; scalar boundary parameters are not implemented yet`,
          );
      }
      const ownsResult = resultRecord
        ? resultOwnership(declaration, writtenResult)
        : false;
      functions.push({
        name: declaration.name.text,
        parameters,
        ...(classes.has(writtenResult) ? { result: writtenResult } : {}),
        ...(ownsResult ? { owns_result: true } : {}),
      });
    }

    for (const [name, declaration] of classes) {
      if (hasDecorator(declaration.decorators, "bind")) boundaryTypes.add(name);
    }
    if (boundaryTypes.size == 0) return;

    const types: WireType[] = [];
    for (const name of [...boundaryTypes].sort()) {
      const declaration = classes.get(name);
      if (!declaration) throw new Error(`unknown binding record ${name}`);
      types.push(this.lowerClass(declaration));
    }
    functions.sort((a, b) => a.name.localeCompare(b.name));

    const projectRoot = process.cwd();
    const packageName = sanitizePackage(
      process.env.WAGO_BIND_PACKAGE || "bindings",
    );
    const schemaOutput = path.resolve(
      projectRoot,
      process.env.WAGO_BIND_SCHEMA || path.join("build", "wago-bind.json"),
    );
    const manifestOutput = path.resolve(
      projectRoot,
      process.env.WAGO_BIND_MANIFEST ||
        path.join("build", "wago-bind.manifest.json"),
    );
    const goOutput = path.resolve(
      projectRoot,
      process.env.WAGO_BIND_GO ||
        path.join("bindings", "wago_bindings.bind.go"),
    );
    writeAtomic(
      schemaOutput,
      JSON.stringify({ package: packageName, types, functions }, null, 2) +
        "\n",
    );
    runGenerator(schemaOutput, manifestOutput, goOutput, packageName);

    const manifest = JSON.parse(readFileSync(manifestOutput, "utf8")) as {
      fingerprint: string;
      types: { name: string; size: number }[];
    };
    if (!/^[0-9a-f]{32}$/.test(manifest.fingerprint))
      throw new Error("wago-bind-as generator returned an invalid fingerprint");
    if (functions.length != 0)
      parser.parseFile(
        runtimeSource(manifest.fingerprint),
        "__wbas_generated.ts",
        true,
      );
    if (functions.some((fn) => fn.result)) {
      const wrapperPath = "__wbas_wrappers.ts";
      const wrappers = functions.filter((fn) => fn.result);
      parser.parseFile(
        wrapperSource(
          wrappers,
          new Map(manifest.types.map((type) => [type.name, type.size])),
        ),
        wrapperPath,
        false,
      );
      const generated = parser.sources.find(
        (source) => source.normalizedPath == wrapperPath,
      );
      if (!generated || generated.statements.length != wrappers.length)
        throw new Error("wago-bind-as could not parse generated call wrappers");
      for (let i = 0; i < wrappers.length; i++) {
        const target = declarations.find(
          (declaration) => declaration.name.text == wrappers[i].name,
        )?.range.source;
        if (!target)
          throw new Error(`missing binding source for ${wrappers[i].name}`);
        retargetRanges(generated.statements[i], target);
        target.statements.push(generated.statements[i]);
      }
      parser.sources.splice(parser.sources.indexOf(generated), 1);
    }
  }

  private lowerClass(declaration: ClassDeclaration): WireType {
    if (
      declaration.extendsType ||
      declaration.implementsTypes?.length ||
      declaration.typeParameters?.length
    )
      throw new Error(
        `binding class ${declaration.name.text} cannot extend, implement, or be generic`,
      );
    const fields: WireField[] = [];
    for (const member of declaration.members) {
      if (member.kind == NodeKind.MethodDeclaration) continue;
      if (member.kind != NodeKind.FieldDeclaration)
        throw new Error(
          `binding class ${declaration.name.text} contains an unsupported member`,
        );
      const field = member as FieldDeclaration;
      if (field.is(CommonFlags.Static)) continue;
      if (!field.type)
        throw new Error(
          `binding field ${declaration.name.text}.${field.name.text} needs an explicit type`,
        );
      if (field.initializer)
        throw new Error(
          `binding field ${declaration.name.text}.${field.name.text} cannot have a field initializer; use a constructor`,
        );
      const written = printType(field.type);
      if (!scalarTypes.has(written))
        throw new Error(
          `binding field ${declaration.name.text}.${field.name.text} uses ${written}; the automatic function facade currently accepts scalar record fields`,
        );
      fields.push({ name: field.name.text, type: schemaType(written) });
    }
    if (fields.length == 0)
      throw new Error(
        `binding class ${declaration.name.text} needs at least one instance field`,
      );
    if (!hasDecorator(declaration.decorators, "unmanaged")) {
      const range = declaration.range;
      const unmanaged = Node.createDecorator(
        Node.createIdentifierExpression("unmanaged", range),
        null,
        range,
      );
      declaration.decorators = [
        unmanaged,
        ...(declaration.decorators || []).filter(
          (decorator) => decoratorName(decorator) != "bind",
        ),
      ];
    } else {
      declaration.decorators = (declaration.decorators || []).filter(
        (decorator) => decoratorName(decorator) != "bind",
      );
    }
    return { name: declaration.name.text, fields };
  }
}

function abortSource(): string {
  return `export default function __wbas_abort(
  message: string | null = null,
  fileName: string | null = null,
  lineNumber: u32 = 0,
  columnNumber: u32 = 0,
): void {
  unreachable();
}
`;
}

function retargetRanges(root: Node, source: Source): void {
  const seen = new Set<Node>();
  const visit = (node: Node): void => {
    if (seen.has(node)) return;
    seen.add(node);
    node.range.source = source;
    for (const value of Object.values(node)) {
      if (value instanceof Node) visit(value);
      else if (Array.isArray(value))
        for (const child of value) if (child instanceof Node) visit(child);
    }
  };
  visit(root);
}

function resultOwnership(
  declaration: FunctionDeclaration,
  resultType: string,
): boolean {
  if (!declaration.body || declaration.body.kind != NodeKind.Block)
    throw new Error(
      `binding ${declaration.name.text} must use a block body so result ownership is explicit`,
    );
  const statements = (declaration.body as BlockStatement).statements;
  const returns = statements.filter(
    (statement) => statement.kind == NodeKind.Return,
  ) as ReturnStatement[];
  if (returns.length != 1 || statements[statements.length - 1] != returns[0])
    throw new Error(
      `binding ${declaration.name.text} must end in one direct return until branch result ownership is implemented`,
    );
  const value = returns[0].value;
  if (!value) throw new Error(`binding ${declaration.name.text} has no result`);
  if (value.kind == NodeKind.New)
    return printTypeName((value as NewExpression).typeName) == resultType;
  if (value.kind == NodeKind.Identifier) {
    const name = (value as IdentifierExpression).text;
    if (
      declaration.signature.parameters.some(
        (parameter) =>
          parameter.name.text == name &&
          printType(parameter.type) == resultType,
      )
    )
      return false;
    throw new Error(
      `binding ${declaration.name.text} can only return a borrowed parameter or directly construct ${resultType}`,
    );
  }
  throw new Error(
    `binding ${declaration.name.text} must return a parameter or directly construct ${resultType}`,
  );
}

function collectDeclarations(
  statements: Statement[],
  classes: Map<string, ClassDeclaration>,
  functions: FunctionDeclaration[],
): void {
  for (const statement of statements) {
    if (statement.kind == NodeKind.ClassDeclaration) {
      const declaration = statement as ClassDeclaration;
      classes.set(declaration.name.text, declaration);
    } else if (statement.kind == NodeKind.FunctionDeclaration) {
      functions.push(statement as FunctionDeclaration);
    } else if (statement.kind == NodeKind.NamespaceDeclaration) {
      const members = (statement as unknown as { members: Statement[] })
        .members;
      if (members) collectDeclarations(members, classes, functions);
    }
  }
}

function runGenerator(
  schema: string,
  manifest: string,
  goOutput: string,
  packageName: string,
): void {
  const result = spawnSync(
    process.env.WAGO_BIND_GO_COMMAND || "go",
    [
      "run",
      "./cmd/wago-bind-as",
      "generate",
      "-schema",
      schema,
      "-manifest",
      manifest,
      "-go",
      goOutput,
      "-package",
      packageName,
    ],
    { cwd: toolRoot(), encoding: "utf8" },
  );
  if (result.status != 0)
    throw new Error(
      `wago-bind-as Go generation failed: ${result.stderr || result.stdout}`,
    );
}

function runGoSync(projectRoot: string): void {
  const result = spawnSync(
    process.env.WAGO_BIND_GO_COMMAND || "go",
    ["run", "./cmd/wago-bind-as", "sync-go", "-root", projectRoot],
    { cwd: toolRoot(), encoding: "utf8" },
  );
  if (result.status != 0)
    throw new Error(
      `wago-bind-as Go-to-AS synchronization failed: ${result.stderr || result.stdout}`,
    );
}

function toolRoot(): string {
  return path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
}

function hasGoBindDirective(directory: string): boolean {
  let entries;
  try {
    entries = readdirSync(directory, { withFileTypes: true });
  } catch {
    return false;
  }
  for (const entry of entries) {
    if (
      entry.isDirectory() &&
      (entry.name == ".git" ||
        entry.name == "build" ||
        entry.name == "node_modules" ||
        entry.name == "vendor")
    )
      continue;
    const child = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      if (hasGoBindDirective(child)) return true;
    } else if (entry.isFile() && entry.name.endsWith(".go")) {
      try {
        if (readFileSync(child, "utf8").includes("//wago:bind ")) return true;
      } catch {}
    }
  }
  return false;
}

function runtimeSource(fingerprint: string): string {
  const first = littleEndianWord(fingerprint.slice(0, 16));
  const second = littleEndianWord(fingerprint.slice(16));
  return (
    `// Generated in memory by wago-bind-as.\n` +
    `export function __wbas_abi_0(): u64 { return 0x${first}; }\n` +
    `export function __wbas_abi_1(): u64 { return 0x${second}; }\n` +
    `export function __wbas_alloc(size: u32): u32 { return <u32>heap.alloc(size); }\n`
  );
}

function wrapperSource(
  functions: WireFunction[],
  typeSizes: Map<string, number>,
): string {
  let source = `// Generated in memory by wago-bind-as.\n`;
  for (const fn of functions) {
    if (!fn.result) continue;
    const resultSize = typeSizes.get(fn.result);
    if (resultSize === undefined)
      throw new Error(`unknown binding result ${fn.result}`);
    const parameters = fn.parameters
      .map((parameter) => `${parameter.name}: ${parameter.type}`)
      .join(", ");
    const separator = parameters.length == 0 ? "" : ", ";
    const argumentsList = fn.parameters
      .map((parameter) => parameter.name)
      .join(", ");
    source +=
      `export function __wbas_call_${fn.name}(${parameters}${separator}__result: ${fn.result}): void {\n` +
      `  const __value = ${fn.name}(${argumentsList});\n` +
      `  memory.copy(changetype<usize>(__result), changetype<usize>(__value), ${resultSize});\n`;
    if (fn.owns_result) source += `  heap.free(changetype<usize>(__value));\n`;
    source += `}\n`;
  }
  return source;
}

function littleEndianWord(hex: string): string {
  return hex.match(/../g)!.reverse().join("");
}

function writeAtomic(output: string, contents: string): void {
  mkdirSync(path.dirname(output), { recursive: true });
  try {
    if (readFileSync(output, "utf8") == contents) return;
  } catch {}
  const temporary = `${output}.${process.pid}.tmp`;
  try {
    writeFileSync(temporary, contents);
    renameSync(temporary, output);
  } finally {
    rmSync(temporary, { force: true });
  }
}

function hasDecorator(
  decorators: DecoratorNode[] | null,
  name: string,
): boolean {
  return (
    decorators?.some((decorator) => decoratorName(decorator) == name) || false
  );
}

function decoratorName(decorator: DecoratorNode): string {
  return decorator.name.kind == NodeKind.Identifier
    ? (decorator.name as IdentifierExpression).text
    : "";
}

function printType(type: TypeNode): string {
  if (type.kind != NodeKind.NamedType)
    throw new Error("binding fields and signatures require named value types");
  const named = type as NamedTypeNode;
  if (named.isNullable)
    throw new Error(
      "binding fields and signatures cannot use native nullable types",
    );
  const name = printTypeName(named.name);
  if (!named.typeArguments?.length) return name;
  return `${name}<${named.typeArguments.map(printType).join(",")}>`;
}

function printTypeName(name: TypeName): string {
  let output = name.identifier.text;
  for (let next = name.next; next; next = next.next)
    output += `.${next.identifier.text}`;
  return output;
}

function schemaType(type: string): string {
  switch (type) {
    case "Utf8":
      return "utf8";
    case "Bytes":
      return "bytes";
    case "TimestampNanos":
      return "timestamp_ns";
    case "DurationNanos":
      return "duration_ns";
    default:
      return type;
  }
}

function sanitizePackage(value: string): string {
  const output = value.replace(/[^A-Za-z0-9_]/g, "_");
  return /^[A-Za-z_]/.test(output) ? output : `_${output}`;
}
