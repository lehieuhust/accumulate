package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/AccumulateNetwork/accumulate/tools/internal/typegen"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var flags struct {
	Package string
	Out     string
	IsState bool
}

func main() {
	cmd := cobra.Command{
		Use:  "gentypes [file]",
		Args: cobra.ExactArgs(1),
		Run:  run,
	}

	cmd.Flags().StringVar(&flags.Package, "package", "protocol", "Package name")
	cmd.Flags().StringVarP(&flags.Out, "out", "o", "types_gen.go", "Output file")
	cmd.Flags().BoolVar(&flags.IsState, "is-state", false, "Is this the state package?")

	_ = cmd.Execute()
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}

func check(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}

func checkf(err error, format string, otherArgs ...interface{}) {
	if err != nil {
		fatalf(format+": %v", append(otherArgs, err)...)
	}
}

func readTypes(file string) typegen.Types {
	f, err := os.Open(file)
	check(err)
	defer f.Close()

	var types map[string]*typegen.Type

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	err = dec.Decode(&types)
	check(err)

	return typegen.TypesFrom(types)
}

func methodName(typ, name string) string {
	return "encoding." + strings.Title(typ) + name
}

func fieldError(op, name string, args ...string) string {
	args = append(args, "err")
	return fmt.Sprintf("fmt.Errorf(\"error %s %s: %%w\", %s)", op, name, strings.Join(args, ","))
}

func needsCustomJSON(typ *typegen.Type) bool {
	for _, f := range typ.Fields {
		if jsonType(f) != "" {
			return true
		}
	}
	return false
}

func run(_ *cobra.Command, args []string) {
	w := new(bytes.Buffer)
	fmt.Fprintf(w, "package %s\n\n", flags.Package)
	fmt.Fprintf(w, "// GENERATED BY go run ./internal/cmd/genmarshal. DO NOT EDIT.\n\n")
	if !flags.IsState {
		fmt.Fprintf(w, `import "github.com/AccumulateNetwork/accumulate/types/state"`+"\n")
	}
	fmt.Fprintf(w, `import (
		"bytes"
		"encoding/json"
		"fmt"
		"math/big"
		"time"

		"github.com/AccumulateNetwork/accumulate/internal/encoding"
		"github.com/AccumulateNetwork/accumulate/types"
		"github.com/AccumulateNetwork/accumulate/internal/url"
		"github.com/AccumulateNetwork/accumulate/protocol"
	)`+"\n\n")

	types := readTypes(args[0])

	for _, typ := range types {
		fmt.Fprintf(w, "type %s struct {\n", typ.Name)
		if typ.Kind == "chain" {
			if flags.IsState {
				fmt.Fprintf(w, "\nChainHeader\n")
			} else {
				fmt.Fprintf(w, "\nstate.ChainHeader\n")
			}
		}

		for _, e := range typ.Embeddings {
			fmt.Fprintf(w, "\t\t%s\n", e)
		}
		for _, field := range typ.Fields {
			formatField(w, field, field.Name, false)
		}
		fmt.Fprintf(w, "}\n\n")
	}

	for _, typ := range types {
		if typ.Kind != "chain" || typ.OmitNewFunc {
			continue
		}
		fmt.Fprintf(w, `func New%s() *%[1]s {
			v := new(%[1]s)
			v.Type = %s
			return v
		}`+"\n\n", typ.Name, typ.GoChainType())
	}

	for _, typ := range types {
		if typ.Kind != "tx" {
			continue
		}
		fmt.Fprintf(w, "func (*%s) GetType() types.TransactionType { return %s }\n\n", typ.Name, typ.GoTxType())
	}

	for _, typ := range types {
		if typ.Incomparable {
			continue
		}

		fmt.Fprintf(w, "func (v *%s) Equal(u *%[1]s) bool {\n", typ.Name)

		if typ.Kind == "chain" {
			fmt.Fprintf(w, "\tif !v.ChainHeader.Equal(&u.ChainHeader) { return false }\n\n")
		}

		for _, field := range typ.Fields {
			if field.MarshalAs == "none" {
				continue
			}
			err := areEqual(w, field, "v."+field.Name, "u."+field.Name)
			checkf(err, "building Equal for %q", typ.Name)
		}

		fmt.Fprintf(w, "\n\treturn true\n}\n\n")
	}

	for _, typ := range types {
		if typ.NonBinary {
			continue
		}

		fmt.Fprintf(w, "func (v *%s) BinarySize() int {\n", typ.Name)
		fmt.Fprintf(w, "\tvar n int\n\n")

		switch typ.Kind {
		case "tx":
			fmt.Fprintf(w, "\nn += encoding.UvarintBinarySize(%s.ID())\n\n", typ.GoTxType())
		case "chain":
			fmt.Fprintf(w, "\t// Enforce sanity\n\tv.Type = %s\n", typ.GoChainType())
			fmt.Fprintf(w, "\nn += v.ChainHeader.GetHeaderSize()\n\n")
		}

		for _, embed := range typ.Embeddings {
			fmt.Fprintf(w, "\tn += v.%s.BinarySize()\n\n", embed)
		}

		for _, field := range typ.Fields {
			if field.MarshalAs == "none" {
				continue
			}
			err := binarySize(w, field, "v."+field.Name)
			checkf(err, "building BinarySize for %q", typ.Name)
		}

		fmt.Fprintf(w, "\n\treturn n\n}\n\n")
	}

	for _, typ := range types {
		if typ.NonBinary {
			continue
		}

		fmt.Fprintf(w, "func (v *%s) MarshalBinary() ([]byte, error) {\n", typ.Name)
		fmt.Fprintf(w, "\tvar buffer bytes.Buffer\n\n")

		switch typ.Kind {
		case "tx":
			fmt.Fprintf(w, "\tbuffer.Write(encoding.UvarintMarshalBinary(%s.ID()))\n\n", typ.GoTxType())
		case "chain":
			fmt.Fprintf(w, "\t// Enforce sanity\n\tv.Type = %s\n\n", typ.GoChainType())
			err := fieldError("encoding", "header")
			fmt.Fprintf(w, "\tif b, err := v.ChainHeader.MarshalBinary(); err != nil { return nil, %s } else { buffer.Write(b) }\n", err)
		}

		for _, embed := range typ.Embeddings {
			fmt.Fprintf(w, "\tif b, err := v.%s.MarshalBinary(); err != nil { return nil, err } else { buffer.Write(b) }\n\n", embed)
		}

		for _, field := range typ.Fields {
			if field.MarshalAs == "none" {
				continue
			}
			err := binaryMarshalValue(w, field, "v."+field.Name, field.Name)
			checkf(err, "building MarshalBinary for %q", typ.Name)
		}

		fmt.Fprintf(w, "\n\treturn buffer.Bytes(), nil\n}\n\n")
	}

	for _, typ := range types {
		if typ.NonBinary {
			continue
		}

		fmt.Fprintf(w, "func (v *%s) UnmarshalBinary(data []byte) error {\n", typ.Name)

		switch typ.Kind {
		case "tx":
			err := fieldError("decoding", "TX type")
			fmt.Fprintf(w, "\ttyp := %s\n", typ.GoTxType())
			fmt.Fprintf(w, "\tif v, err := encoding.UvarintUnmarshalBinary(data); err != nil { return %s } else if v != uint64(typ) { return fmt.Errorf(\"invalid TX type: want %%v, got %%v\", typ, types.TransactionType(v)) }\n", err)
			fmt.Fprintf(w, "\tdata = data[encoding.UvarintBinarySize(uint64(typ)):]\n\n")

		case "chain":
			err := fieldError("decoding", "header")
			fmt.Fprintf(w, "\ttyp := %s\n", typ.GoChainType())
			fmt.Fprintf(w, "\tif err := v.ChainHeader.UnmarshalBinary(data); err != nil { return %s } else if v.Type != typ { return fmt.Errorf(\"invalid chain type: want %%v, got %%v\", typ, v.Type) }\n", err)
			fmt.Fprintf(w, "\tdata = data[v.GetHeaderSize():]\n\n")
		}

		for _, embed := range typ.Embeddings {
			fmt.Fprintf(w, "\tif err := v.%s.UnmarshalBinary(data); err != nil { return err }\n", embed)
			fmt.Fprintf(w, "\tdata = data[v.%s.BinarySize():]\n\n", embed)
		}

		for _, field := range typ.Fields {
			if field.MarshalAs == "none" {
				continue
			}
			err := binaryUnmarshalValue(w, field, "v."+field.Name, field.Name)
			checkf(err, "building UnmarshalBinary for %q", typ.Name)
		}

		fmt.Fprintf(w, "\n\treturn nil\n}\n\n")
	}

	for _, typ := range types {
		if !needsCustomJSON(typ) {
			continue
		}

		fmt.Fprintf(w, "func (v *%s) MarshalJSON() ([]byte, error) {\n", typ.Name)
		jsonVar(w, typ, "u")

		if typ.Kind == "chain" {
			fmt.Fprintf(w, "\tu.ChainHeader = v.ChainHeader\n")
		}
		for _, e := range typ.Embeddings {
			fmt.Fprintf(w, "\tu.%s = v.%[1]s\n", e)
		}
		for _, f := range typ.Fields {
			if f.MarshalAs == "none" {
				continue
			}
			valueToJson(w, f, "u."+f.Name, "v."+f.Name)
			if f.Alternative != "" {
				valueToJson(w, f, "u."+f.Alternative, "v."+f.Name)
			}
		}

		fmt.Fprintf(w, "\treturn json.Marshal(&u)\t")
		fmt.Fprintf(w, "}\n\n")
	}

	for _, typ := range types {
		if !needsCustomJSON(typ) {
			continue
		}

		fmt.Fprintf(w, "func (v *%s) UnmarshalJSON(data []byte) error {\t", typ.Name)
		jsonVar(w, typ, "u")

		if typ.Kind == "chain" {
			fmt.Fprintf(w, "\tu.ChainHeader = v.ChainHeader\n")
		}
		for _, e := range typ.Embeddings {
			fmt.Fprintf(w, "\tu.%s = v.%[1]s\n", e)
		}
		for _, f := range typ.Fields {
			if f.MarshalAs == "none" {
				continue
			}
			valueToJson(w, f, "u."+f.Name, "v."+f.Name)
			if f.Alternative != "" {
				valueToJson(w, f, "u."+f.Alternative, "v."+f.Name)
			}
		}

		fmt.Fprintf(w, "\tif err := json.Unmarshal(data, &u); err != nil {\n\t\treturn err\n\t}\n")

		if typ.Kind == "chain" {
			fmt.Fprintf(w, "\tv.ChainHeader = u.ChainHeader\n")
		}
		for _, e := range typ.Embeddings {
			fmt.Fprintf(w, "\tv.%s = u.%[1]s\n", e)
		}
		for _, f := range typ.Fields {
			if f.MarshalAs == "none" {
				continue
			}
			if f.Alternative == "" {
				valueFromJson(w, f, "v."+f.Name, "u."+f.Name, f.Name)
				continue
			}

			fmt.Fprintf(w, "\tvar zero%s %s\n", f.Name, resolveType(f, false))
			fmt.Fprintf(w, "\tif u.%s != zero%[1]s {\n", f.Name)
			valueFromJson(w, f, "v."+f.Name, "u."+f.Name, f.Name)
			fmt.Fprintf(w, "\t} else {\n")
			valueFromJson(w, f, "v."+f.Name, "u."+f.Alternative, f.Name)
			fmt.Fprintf(w, "\t}\n")
		}

		fmt.Fprintf(w, "\treturn nil\t")
		fmt.Fprintf(w, "}\n\n")
	}

	typegen.GoFmt(flags.Out, w)
}
