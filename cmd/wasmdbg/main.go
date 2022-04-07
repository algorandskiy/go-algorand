package main

import (
	"fmt"
	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/data/transactions"
	"github.com/algorand/go-algorand/data/transactions/logic"
	"github.com/algorand/go-algorand/protocol"
	"strings"
	"syscall/js"
)

/*
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
*/

func callEval() js.Func {
	evalFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) != 1 {
			return "Invalid no of arguments passed"
		}
		inputProgram := args[0].String()
		fmt.Printf("input %s\n", inputProgram)
		opStream, err := logic.AssembleString(inputProgram)
		if err != nil {
			fmt.Printf("Unable to assemble input program to OpStream %s\n", err)
			return err.Error()
		}
		if opStream.HasStatefulOps {
			fmt.Printf("Cannot evaluate stateful programs yet.")
			return "Cannot evaluate stateful programs yet."
		} else {
			var txnGroup []transactions.SignedTxn
			txnGroup = append(txnGroup, transactions.SignedTxn{})
			signedTxnGroup := transactions.WrapSignedTxnsWithAD(txnGroup)
			signedTxnGroup[0].Lsig.Logic = opStream.Program
			proto := config.Consensus[protocol.ConsensusCurrentVersion]
			var sb strings.Builder
			evalParams := &logic.EvalParams{
				Proto:    &proto,
				Trace:    &sb,
				TxnGroup: signedTxnGroup,
			}
			_, err := logic.EvalSignature(0, evalParams)
			if err != nil {
				return err.Error()
			}
			return evalParams.Trace.String()
		}
	})
	return evalFunc
}

func main() {
	js.Global().Set("callEval", callEval())
	<-make(chan bool)
}
