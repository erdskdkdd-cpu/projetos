// HTTP server for the Luau bytecode decompiler.
// Accepts bytecode via POST and returns decompiled Luau source.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	bc "Geckocompiler/bytecode"
	"Geckocompiler/codegen"
	"Geckocompiler/deserializer"
	"Geckocompiler/lifter"
)

// DecompileRequest represents the JSON body for the /decompile endpoint.
type DecompileRequest struct {
	// Bytecode as base64-encoded string
	BytecodeBase64 string `json:"bytecode_base64"`
	// Bytecode as hex string (alternative)
	BytecodeHex string `json:"bytecode_hex"`
}

// DecompileResponse is the JSON response from the /decompile endpoint.
type DecompileResponse struct {
	Success    bool    `json:"success"`
	Source     string  `json:"source,omitempty"`
	Error      string  `json:"error,omitempty"`
	DeserTime  float64 `json:"deser_time_ms"`
	LiftTime   float64 `json:"lift_time_ms"`
	GenTime    float64 `json:"gen_time_ms"`
	StringCount int    `json:"string_count"`
	ProtoCount  int    `json:"proto_count"`
}

func main() {
	port := "5000"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	http.HandleFunc("/decompile", handleDecompile)
	http.HandleFunc("/health", handleHealth)

	addr := "0.0.0.0:" + port
	log.Printf("Geckocompiler server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleDecompile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var data []byte

	// Try JSON body first
	contentType := r.Header.Get("Content-Type")
	if contentType == "application/json" {
		var req DecompileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if req.BytecodeBase64 != "" {
			decoded, err := base64.StdEncoding.DecodeString(req.BytecodeBase64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid base64: "+err.Error())
				return
			}
			data = decoded
		} else if req.BytecodeHex != "" {
			decoded, err := bc.HexToBytes(req.BytecodeHex)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid hex: "+err.Error())
				return
			}
			data = decoded
		}
	}

	// Fall back to raw body
	if data == nil {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read body: "+err.Error())
			return
		}
		data = autoDetectServerInput(raw)
	}

	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "empty bytecode")
		return
	}

	resp := decompileBytecode(data)
	json.NewEncoder(w).Encode(resp)
}

func decompileBytecode(data []byte) DecompileResponse {
	// Deserialize
	startTime := time.Now()
	bytecodeObj, err := deserializer.Deserialize(data)
	if err != nil {
		return DecompileResponse{Success: false, Error: fmt.Sprintf("deserialize: %v", err)}
	}
	deserTime := float64(time.Since(startTime).Microseconds()) / 1000.0

	// Lift
	startTime = time.Now()
	lift := lifter.NewLifter(bytecodeObj)
	mainAST := lift.LiftAll()
	liftTime := float64(time.Since(startTime).Microseconds()) / 1000.0

	// Generate
	startTime = time.Now()
	gen := codegen.NewCodeGen()
	output := gen.Generate(mainAST)
	genTime := float64(time.Since(startTime).Microseconds()) / 1000.0

	header := fmt.Sprintf(
		"-- Decompiled with Geckocompiler (Go)\n"+
			"-- Luau version %d, Types version %d\n"+
			"-- %d strings, %d protos\n\n",
		bytecodeObj.Version, bytecodeObj.TypesVersion,
		len(bytecodeObj.Strings), len(bytecodeObj.Protos),
	)

	return DecompileResponse{
		Success:     true,
		Source:      header + output,
		DeserTime:   deserTime,
		LiftTime:    liftTime,
		GenTime:     genTime,
		StringCount: len(bytecodeObj.Strings),
		ProtoCount:  len(bytecodeObj.Protos),
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(DecompileResponse{Success: false, Error: msg})
}

func autoDetectServerInput(raw []byte) []byte {
	if len(raw) > 0 && raw[0] < 10 {
		return raw
	}
	if decoded, err := base64.StdEncoding.DecodeString(string(raw)); err == nil && len(decoded) > 0 {
		return decoded
	}
	if decoded, err := bc.HexToBytes(string(raw)); err == nil && len(decoded) > 0 {
		return decoded
	}
	return raw
}
