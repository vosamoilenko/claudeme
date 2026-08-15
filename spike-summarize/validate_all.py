#!/usr/bin/env python3
import json, glob, sys
from jsonschema import validate, ValidationError

schema = json.load(open("schema.json"))
rows = []
for path in sorted(glob.glob("results/*/*.raw.json")):
    parts = path.split("/")
    model, tname = parts[1], parts[2].replace(".raw.json", "")
    try:
        data = json.load(open(path))
    except Exception as e:
        rows.append((model, tname, "PARSE_FAIL", str(e)))
        continue
    try:
        validate(instance=data, schema=schema)
        rows.append((model, tname, "VALID", ""))
    except ValidationError as e:
        rows.append((model, tname, "SCHEMA_FAIL", str(e).split("\n")[0]))

for r in rows:
    print(r[0], r[1], r[2], r[3])
