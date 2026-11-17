// Reference check: every example under examples/<schema>/valid must pass its JSON Schema,
// and every example under examples/<schema>/invalid named "schema-*" (or any invalid
// example of a schema without cross-field rules) must fail it. Plan examples named
// "rule-*" pass the schema on purpose: they break rules JSON Schema cannot express,
// which agents-python checks (app/contract/rules.py).
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import fs from "node:fs";
import path from "node:path";

const here = path.dirname(new URL(import.meta.url).pathname);
const ajv = new Ajv2020({ allErrors: true, strict: true });
addFormats(ajv);
// Integer formats used by the tool schema so typify picks the Rust integer type.
const intFormat = (min, max) => ({ type: "number", validate: (n) => Number.isInteger(n) && n >= min && n <= max });
ajv.addFormat("uint32", intFormat(0, 2 ** 32 - 1));
ajv.addFormat("int32", intFormat(-(2 ** 31), 2 ** 31 - 1));
ajv.addFormat("uint", intFormat(0, Number.MAX_SAFE_INTEGER));
ajv.addFormat("int64", intFormat(Number.MIN_SAFE_INTEGER, Number.MAX_SAFE_INTEGER));
const schemas = {};
for (const f of fs.readdirSync(here).filter((f) => f.endsWith(".schema.json"))) {
  const s = JSON.parse(fs.readFileSync(path.join(here, f)));
  ajv.addSchema(s);
  schemas[f.replace(".schema.json", "")] = s;
}

let failures = 0;
for (const name of fs.readdirSync(path.join(here, "examples"))) {
  const schema = schemas[name];
  if (!schema) throw new Error(`examples/${name} has no ${name}.schema.json`);
  for (const kind of ["valid", "invalid"]) {
    const dir = path.join(here, "examples", name, kind);
    if (!fs.existsSync(dir)) continue;
    for (const f of fs.readdirSync(dir).sort()) {
      let doc = JSON.parse(fs.readFileSync(path.join(dir, f)));
      let validate = ajv.getSchema(schema.$id);
      if (name === "tools") {
        validate = ajv.getSchema(`${schema.$id}#/$defs/${doc.$def}`);
        doc = doc.value;
      }
      const ok = validate(doc);
      const expectOk = kind === "valid" || f.startsWith("rule-");
      const status = ok === expectOk ? "ok  " : "FAIL";
      if (ok !== expectOk) failures++;
      const why = ok ? "" : `  (${validate.errors[0].instancePath || "/"} ${validate.errors[0].message})`;
      console.log(`${status} ${ok ? "accepted" : "rejected"}  ${name}/${kind}/${f}${why}`);
    }
  }
}
if (failures) {
  console.error(`${failures} example(s) did not match their folder`);
  process.exit(1);
}
