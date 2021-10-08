package main

import (
	"fmt"
)

func (g *Generator) genTypeScriptCodes(typeName string, values []Value, lineComment bool) {
	// generate enum definition
	fmt.Fprintf(&g.tsBuf, "export enum %s {\n", typeName)
	for _, v := range values {
		fieldValue := v.name
		if lineComment {
			fieldValue = v.comment
		}
		fmt.Fprintf(&g.tsBuf, "  %s = '%s',\n", v.name, fieldValue)
	}
	fmt.Fprintf(&g.tsBuf, "  UNRECOGNIZED = 'UNRECOGNIZED',\n")
	fmt.Fprint(&g.tsBuf, "}\n\n")

	// generate enum value to enum conversion
	fmt.Fprintf(&g.tsBuf, "export const %sFromJSON = (object: any) => {\n", typeName)
	fmt.Fprintf(&g.tsBuf, "  switch (object) {\n")
	for _, v := range values {
		fieldValue := v.name
		if lineComment {
			fieldValue = v.comment
		}
		fmt.Fprintf(&g.tsBuf, "    case %d:\n", v.value)
		fmt.Fprintf(&g.tsBuf, "    case '%s':\n", fieldValue)
		fmt.Fprintf(&g.tsBuf, "      return %s.%s;\n", typeName, v.name)
	}
	fmt.Fprintf(&g.tsBuf, "    case -1:\n")
	fmt.Fprintf(&g.tsBuf, "    case 'UNRECOGNIZED':\n")
	fmt.Fprintf(&g.tsBuf, "    default:\n")
	fmt.Fprintf(&g.tsBuf, "      return %s.UNRECOGNIZED;\n", typeName)
	fmt.Fprint(&g.tsBuf, "  }\n}\n")
}
