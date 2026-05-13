// CLI entry point for the Luau bytecode decompiler.
// Usage: decompile <input_file> [-o output.luau] [--dump]
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	bc "Geckocompiler/bytecode"
	"Geckocompiler/codegen"
	"Geckocompiler/deserializer"
	"Geckocompiler/lifter"
)

func main() {
	outputPath := flag.String("o", "", "Output file for decompiled Luau code")
	dumpFlag := flag.Bool("dump", false, "Dump raw bytecode structure")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: decompile <input_file> [-o output.luau] [--dump]")
		os.Exit(1)
	}
	inputPath := flag.Arg(0)

	rawData, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	data := autoDetectFormat(rawData)
	fmt.Printf("Read %d bytes of bytecode\n", len(data))

	// Deserialize
	startTime := time.Now()
	bytecodeObj, err := deserializer.Deserialize(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Deserialization error: %v\n", err)
		os.Exit(1)
	}
	deserTime := time.Since(startTime)
	fmt.Printf("Deserialized in %.4fs: %d strings, %d protos\n",
		deserTime.Seconds(), len(bytecodeObj.Strings), len(bytecodeObj.Protos))
	fmt.Printf("Main proto ID: %d\n", bytecodeObj.MainProtoID)

	if *dumpFlag {
		dumpBytecode(bytecodeObj)
		return
	}

	// Lift to AST
	startTime = time.Now()
	lift := lifter.NewLifter(bytecodeObj)
	mainAST := lift.LiftAll()
	liftTime := time.Since(startTime)

	// Generate code
	startTime = time.Now()
	gen := codegen.NewCodeGen()
	output := gen.Generate(mainAST)
	genTime := time.Since(startTime)

	header := fmt.Sprintf(
		"-- Decompiled with Geckocompiler (Go)\n"+
			"-- Luau version %d, Types version %d\n"+
			"-- %d strings, %d protos\n"+
			"-- Deserialized in %.4fs, Lifted in %.4fs, Generated in %.4fs\n\n",
		bytecodeObj.Version, bytecodeObj.TypesVersion,
		len(bytecodeObj.Strings), len(bytecodeObj.Protos),
		deserTime.Seconds(), liftTime.Seconds(), genTime.Seconds(),
	)
	result := header + output

	if *outputPath != "" {
		if err := os.WriteFile(*outputPath, []byte(result), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Output written to %s\n", *outputPath)
	} else {
		fmt.Println("\n" + "============================================================")
		fmt.Println(result)
	}
}

// autoDetectFormat determines whether input is raw binary or hex-encoded.
func autoDetectFormat(raw []byte) []byte {
	if len(raw) > 0 && raw[0] < 10 {
		return raw // raw binary — Luau versions are < 10
	}
	// Try hex decode
	text := string(raw)
	decoded, err := bc.HexToBytes(text)
	if err == nil && len(decoded) > 0 {
		return decoded
	}
	return raw // fallback to raw
}

func dumpBytecode(bytecodeObj *bc.Bytecode) {
	fmt.Printf("=== Luau Bytecode v%d (Types v%d) ===\n", bytecodeObj.Version, bytecodeObj.TypesVersion)
	fmt.Printf("Strings: %d\n", len(bytecodeObj.Strings))
	for i, s := range bytecodeObj.Strings {
		fmt.Printf("  [%d] \"%s\"\n", i+1, s)
	}
	fmt.Printf("\nProtos: %d\n", len(bytecodeObj.Protos))
	for _, proto := range bytecodeObj.Protos {
		fmt.Printf("\n--- Proto %d ---\n", proto.ProtoID)
		fmt.Printf("  max_stack=%d params=%d upvals=%d vararg=%v\n",
			proto.MaxStackSize, proto.NumParams, proto.NumUpvalues, proto.IsVararg)
		fmt.Printf("  Instructions: %d\n", len(proto.Instructions))
		for _, inst := range proto.Instructions {
			auxStr := ""
			if inst.HasAux() {
				auxStr = fmt.Sprintf(" AUX=0x%08x", inst.Aux)
			}
			fmt.Printf("    [%3d] %-20s A=%d B=%d C=%d D=%d%s\n",
				inst.PC, inst.OpName, inst.A, inst.B, inst.C, inst.D, auxStr)
		}
		fmt.Printf("  Constants: %d\n", len(proto.Constants))
		for _, k := range proto.Constants {
			fmt.Printf("    [K%d] type=%d: %v\n", k.Index, k.Type, k.Value)
		}
		fmt.Printf("  Children: %v\n", proto.ChildProtos)
	}
	fmt.Printf("\nMain proto: %d\n", bytecodeObj.MainProtoID)
}
