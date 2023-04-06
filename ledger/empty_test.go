package ledger

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/transactions"
	ledgertesting "github.com/algorand/go-algorand/ledger/testing"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/test/partitiontest"
	"github.com/algorand/go-algorand/util/db"
	"github.com/stretchr/testify/require"
)

// TestEmptyAccountUpdate checks empty account update can be committed
func TestEmptyAccountUpdate(t *testing.T) {
	partitiontest.PartitionTest(t)

	genesisInitState, initKeys := ledgertesting.GenerateInitState(t, protocol.ConsensusCurrentVersion, 2)
	const inMem = true
	log := logging.TestingLog(t)
	cfg := config.GetDefaultLocal()
	cfg.MaxAcctLookback = 1
	l, err := OpenLedger(log, t.Name(), inMem, genesisInitState, cfg)
	require.NoError(t, err, "could not open ledger")
	defer l.Close()

	initAccounts := genesisInitState.Accounts
	var addrList []basics.Address
	for addr := range initAccounts {
		if addr != testPoolAddr && addr != testSinkAddr {
			addrList = append(addrList, addr)
		}
	}
	sender := addrList[0]
	proto := config.Consensus[protocol.ConsensusCurrentVersion]
	receiver := ledgertesting.RandomAddress()
	invalidAddr := ledgertesting.RandomAddress()
	fmt.Printf("rcv: %s\n", receiver.String())
	fmt.Printf("inv: %s\n", invalidAddr.String())

	// castedDB := l.trackerDBs.(*trackerSQLStore)
	db, err := db.OpenPair(t.Name()+".tracker.sqlite", inMem)
	require.NoError(t, err)
	_, err = db.Wdb.Handle.Exec("INSERT INTO accountbase (address, data) VALUES (?, ?)", invalidAddr[:], []byte{})
	require.NoError(t, err)

	for _, rcv := range []basics.Address{receiver, invalidAddr} {
		var stxns []transactions.SignedTxn
		latest := l.Latest()
		feeMult := rand.Intn(5) + 1
		amountMult := rand.Intn(1000) + 1
		txHeader := transactions.Header{
			Sender:      sender,
			Fee:         basics.MicroAlgos{Raw: proto.MinTxnFee * uint64(feeMult)},
			FirstValid:  latest + 1,
			LastValid:   latest + 10,
			GenesisID:   t.Name(),
			GenesisHash: crypto.Hash([]byte(t.Name())),
		}

		correctPayFields := transactions.PaymentTxnFields{
			Receiver: rcv,
			Amount:   basics.MicroAlgos{Raw: uint64(100 * amountMult)},
		}

		tx := transactions.Transaction{
			Type:             protocol.PaymentTx,
			Header:           txHeader,
			PaymentTxnFields: correctPayFields,
		}

		stxns = append(stxns, sign(initKeys, tx))
		err = l.addBlockTxns(t, genesisInitState.Accounts, stxns, transactions.ApplyData{})
		require.NoError(t, err)
	}
	l.WaitForCommit(2)

	addEmptyValidatedBlock(t, l, initAccounts)
	addEmptyValidatedBlock(t, l, initAccounts)

	commitRoundLookback(basics.Round(cfg.MaxAcctLookback), l)

	require.Equal(t, l.Latest()-basics.Round(cfg.MaxAcctLookback), l.trackers.dbRound)

	d, _, _, err := l.LookupAccount(l.Latest(), receiver)
	require.NoError(t, err)
	require.Greater(t, d.MicroAlgos.Raw, uint64(0))

	d, _, _, err = l.LookupAccount(l.Latest(), invalidAddr)
	require.NoError(t, err)
	require.Greater(t, d.MicroAlgos.Raw, uint64(0))
}
