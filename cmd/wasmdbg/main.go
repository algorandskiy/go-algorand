package main

import (
	"fmt"
	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/data/transactions/logic"
	"github.com/algorand/go-algorand/protocol"
	"os"
)

const (
	example_program = `#pragma version 4
// loop 1 - 10
// init loop var
int 0
loop:
int 1
+
// implement loop code
// ...
// check upper bound
int 10
<=
bnz loop`
)

func callEval() {
	opStream, err := logic.AssembleString(example_program)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err)
	}
	proto := config.Consensus[protocol.ConsensusCurrentVersion]
	evalParams := &logic.EvalParams{
		Proto: &proto,
		// Trace: foobar,
		// TxnGroup: foobar,
		// pastScratch: foobar,
		// logger: foobar,
		// Ledger: foobar,
		// Debugger: foobar,
		// MinTealVersion: foobar,
	}
	if opStream.HasStatefulOps {
		fmt.Println("starting EvalContract")
		logic.EvalContract(opStream.Program, 0, 0, evalParams)
		fmt.Println("finished EvalContract")
	} else {
		fmt.Println("starting EvalSignature")
		logic.EvalContract(opStream.Program, 0, 0, evalParams)
		fmt.Println("finished EvalSignature")
	}

}

func main() {
	fmt.Println("executing callEval")
	callEval()
	fmt.Println("done executing")
}
