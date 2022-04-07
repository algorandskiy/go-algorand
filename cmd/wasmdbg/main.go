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
		opStream, err := logic.AssembleString(inputProgram)
		if err != nil {
			return err.Error()
		}
		var outputStr strings.Builder
		if opStream.HasStatefulOps {
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
			success, err := logic.EvalSignature(0, evalParams)
			if err != nil {
				return err.Error()
			}
			fmt.Fprintf(&outputStr, "Program success: %t\n", success)
			fmt.Fprintf(&outputStr, evalParams.Trace.String())
			return outputStr.String()
		}
	})
	return evalFunc
}

func main() {
	js.Global().Set("callEval", callEval())
	<-make(chan bool)
}
