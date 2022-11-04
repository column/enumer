package main

import "fmt"

// Arguments to format are:
//	[1]: type name
const stringNameToValueMethod = `// %[1]sString retrieves an enum value from the enum constants string name.
// Throws an error if the param is not part of the enum.
func %[1]sString(s string) (%[1]s, error) {
	s = strings.TrimSpace(s)
	if val, ok := _%[1]sNameToValueMap[s]; ok {
		return val, nil
	}
	return 0, %[2]s
}
`

// Arguments to format are:
//	[1]: type name
const stringValuesMethod = `// %[1]sValues returns all values of the enum
func %[1]sValues() []%[1]s {
	return _%[1]sValues
}
`

// Arguments to format are:
//	[1]: type name
const stringBelongsMethodLoop = `// IsA%[1]s returns "true" if the value is listed in the enum definition. "false" otherwise
func (i %[1]s) IsA%[1]s() bool {
	for _, v := range _%[1]sValues {
		if i == v {
			return true
		}
	}
	return false
}
`

// Arguments to format are:
//	[1]: type name
const stringBelongsMethodSet = `// IsA%[1]s returns "true" if the value is listed in the enum definition. "false" otherwise
func (i %[1]s) IsA%[1]s() bool {
	_, ok := _%[1]sMap[i] 
	return ok
}
`

func (g *Generator) buildBasicExtras(runs [][]Value, typeName string, runsThreshold int, parseError string) {
	// At this moment, either "g.declareIndexAndNameVars()" or "g.declareNameVars()" has been called

	// Print the slice of values
	g.Printf("\nvar _%sValues = []%s{", typeName, typeName)
	for _, values := range runs {
		for _, value := range values {
			g.Printf("\t%s, ", value.str)
		}
	}
	g.Printf("}\n\n")

	// Print the map between name and value
	g.Printf("\nvar _%sNameToValueMap = map[string]%s{\n", typeName, typeName)
	thereAreRuns := len(runs) > 1 && len(runs) <= runsThreshold
	var n int
	var runID string
	for i, values := range runs {
		if thereAreRuns {
			runID = "_" + fmt.Sprintf("%d", i)
			n = 0
		} else {
			runID = ""
		}

		for _, value := range values {
			g.Printf("\t_%sName%s[%d:%d]: %s,\n", typeName, runID, n, n+len(value.name), &value)
			n += len(value.name)
		}
	}
	g.Printf("}\n\n")

	// Print the basic extra methods
	errString := fmt.Sprintf(`errors.Newf("%%s does not belong to %s values", s)`, typeName)
	if parseError != "" {
		errString = fmt.Sprintf(`apierrors.%s.AddDetail("invalid_value", s)`, parseError)
	}
	g.Printf(stringNameToValueMethod, typeName, errString)
	g.Printf(stringValuesMethod, typeName)
	if len(runs) <= runsThreshold {
		g.Printf(stringBelongsMethodLoop, typeName)
	} else { // There is a map of values, the code is simpler then
		g.Printf(stringBelongsMethodSet, typeName)
	}
}

// Arguments to format are:
//	[1]: type name
const jsonMethods = `
// MarshalJSON implements the json.Marshaler interface for %[1]s
func (i %[1]s) MarshalJSON() ([]byte, error) {
	if !i.IsA%[1]s() {
		return json.Marshal(nil)
	}
	return json.Marshal(i.String())
}

// UnmarshalJSON implements the json.Unmarshaler interface for %[1]s
func (i *%[1]s) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.Newf("%[1]s should be a string, got %%s", data)
	}

	var err error
	*i, err = %[1]sString(s)
	return err
}
`

func (g *Generator) buildJSONMethods(runs [][]Value, typeName string) {
	g.Printf(jsonMethods, typeName)
}

const xmlMethods = `
// UnmarshalXMLAttr implements xml.UnmarshalerAttr interface for %[1]s
func (i *%[1]s) UnmarshalXMLAttr(attr xml.Attr) error {
	v, err := %[1]sString(attr.Value)
	if err != nil {
		return err
	}
	*i = v
	return nil
}

// MarshalXMLAttr implements xml.MarshalerAttr interface for %[1]s
func (i %[1]s) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	attr := xml.Attr{
		Name:  name,
		Value: i.String(),
	}
	return attr, nil
}

// UnmarshalXML implements xml.Unmarshaler interface for %[1]s
func (i *%[1]s) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var s string
	if err := d.DecodeElement(&s, &start); err != nil {
		return err
	}
	v, err := %[1]sString(s)
	if err != nil {
		return err
	}
	*i = v
	return nil
}

// MarshalXML implements xml.Marshaler interface for %[1]s
func (i %[1]s) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	return e.EncodeElement(i.String(), start)
}
`

func (g *Generator) buildXMLMethods(runs [][]Value, typeName string) {
	g.Printf(xmlMethods, typeName)
}

// Arguments to format are:
//	[1]: type name
const textMethods = `
// MarshalText implements the encoding.TextMarshaler interface for %[1]s
func (i %[1]s) MarshalText() ([]byte, error) {
	return []byte(i.String()), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface for %[1]s
func (i *%[1]s) UnmarshalText(text []byte) error {
	var err error
	*i, err = %[1]sString(string(text))
	return err
}
`

func (g *Generator) buildTextMethods(runs [][]Value, typeName string) {
	g.Printf(textMethods, typeName)
}

// Arguments to format are:
//	[1]: type name
const yamlMethods = `
// MarshalYAML implements a YAML Marshaler for %[1]s
func (i %[1]s) MarshalYAML() (interface{}, error) {
	return i.String(), nil
}

// UnmarshalYAML implements a YAML Unmarshaler for %[1]s
func (i *%[1]s) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	var err error
	*i, err = %[1]sString(s)
	return err
}
`

func (g *Generator) buildYAMLMethods(runs [][]Value, typeName string) {
	g.Printf(yamlMethods, typeName)
}
