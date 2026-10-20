// Copyright (C) 2019-2026 Algorand Foundation Ltd.
// This file is part of go-algorand
//
// go-algorand is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// go-algorand is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with go-algorand.  If not, see <https://www.gnu.org/licenses/>.

package agreement

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/committee"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/test/partitiontest"
)

// makeSortedCreds returns n selected credentials in increasing order.
func makeSortedCreds(n int) []committee.Credential {
	creds := make([]committee.Credential, n)
	for i := range creds {
		creds[i] = committee.Credential{Weight: 1, VrfOut: crypto.Hash([]byte{byte(i)})}
	}
	slices.SortFunc(creds, func(a, b committee.Credential) int {
		if a.Less(b) {
			return -1
		}
		if b.Less(a) {
			return 1
		}
		return 0
	})
	return creds
}

func makeAddr(i byte) basics.Address {
	var a basics.Address
	a[0] = i
	return a
}

func makeProposalVote(sender basics.Address, rnd round, per period, cred committee.Credential) vote {
	return vote{R: rawVote{Sender: sender, Round: rnd, Period: per, Step: propose}, Cred: cred}
}

func makeTestCert(rnd round, per period, origPer period, winner basics.Address) Certificate {
	return Certificate{Round: rnd, Period: per, Proposal: proposalValue{OriginalPeriod: origPer, OriginalProposer: winner}}
}

func lowestOf(rnd round, per period, frozen *vote, late *vote) readLowestEvent {
	re := readLowestEvent{T: readLowestVote, Round: rnd, Period: per}
	if frozen != nil {
		re.Vote, re.Filled = *frozen, true
	}
	if late != nil {
		re.LowestIncludingLate, re.HasLowestIncludingLate = *late, true
	}
	return re
}

func TestClassifyProposalOutcome(t *testing.T) {
	partitiontest.PartitionTest(t)

	creds := makeSortedCreds(3)
	low, mid, high := creds[0], creds[1], creds[2]
	us, them, other := makeAddr(1), makeAddr(2), makeAddr(3)
	const rnd = round(100)

	ownMid := []ownProposal{{Sender: us, Period: 0, Cred: mid}}

	t.Run("no own proposals", func(t *testing.T) {
		o := classifyProposalOutcome(nil, makeTestCert(rnd, 0, 0, them), readLowestEvent{})
		require.Equal(t, outcomeNone, o.Kind)
	})

	t.Run("won", func(t *testing.T) {
		v := makeProposalVote(us, rnd, 0, mid)
		o := classifyProposalOutcome(ownMid, makeTestCert(rnd, 0, 0, us), lowestOf(rnd, 0, &v, &v))
		require.Equal(t, outcomeWon, o.Kind)
	})

	t.Run("won reproposal in later period", func(t *testing.T) {
		o := classifyProposalOutcome(ownMid, makeTestCert(rnd, 1, 1, us), lowestOf(rnd, 1, nil, nil))
		require.Equal(t, outcomeWon, o.Kind)
	})

	t.Run("outbid", func(t *testing.T) {
		v := makeProposalVote(them, rnd, 0, low)
		o := classifyProposalOutcome(ownMid, makeTestCert(rnd, 0, 0, them), lowestOf(rnd, 0, &v, &v))
		require.Equal(t, outcomeOutbid, o.Kind)
		require.True(t, o.HasWinnerCred)
	})

	t.Run("winner higher, known from frozen lowest", func(t *testing.T) {
		// the winner froze our filter, then our lower proposal arrived late locally
		winner := makeProposalVote(them, rnd, 0, high)
		ours := makeProposalVote(us, rnd, 0, mid)
		o := classifyProposalOutcome(ownMid, makeTestCert(rnd, 0, 0, them), lowestOf(rnd, 0, &winner, &ours))
		require.Equal(t, outcomeLostWinnerHigher, o.Kind)
		require.Equal(t, high, o.WinnerCred)
		require.False(t, o.fields()["OurLowestBeforeFreeze"].(bool))
	})

	t.Run("third party lower", func(t *testing.T) {
		v := makeProposalVote(other, rnd, 0, low)
		o := classifyProposalOutcome(ownMid, makeTestCert(rnd, 0, 0, them), lowestOf(rnd, 0, &v, &v))
		require.Equal(t, outcomeNotLowest, o.Kind)
		require.False(t, o.HasWinnerCred)
	})

	t.Run("ours lowest observed, other certified", func(t *testing.T) {
		v := makeProposalVote(us, rnd, 0, mid)
		o := classifyProposalOutcome(ownMid, makeTestCert(rnd, 0, 0, them), lowestOf(rnd, 0, &v, &v))
		require.Equal(t, outcomePending, o.Kind)
		require.True(t, o.fields()["OurLowestBeforeFreeze"].(bool))
	})

	t.Run("ours never reached our own tracker", func(t *testing.T) {
		v := makeProposalVote(other, rnd, 0, high)
		o := classifyProposalOutcome(ownMid, makeTestCert(rnd, 0, 0, them), lowestOf(rnd, 0, &v, &v))
		require.Equal(t, outcomePending, o.Kind)
	})

	t.Run("different period", func(t *testing.T) {
		o := classifyProposalOutcome(ownMid, makeTestCert(rnd, 1, 1, them), lowestOf(rnd, 1, nil, nil))
		require.Equal(t, outcomeDifferentPeriod, o.Kind)
	})

	t.Run("best of several local accounts", func(t *testing.T) {
		own := []ownProposal{{Sender: us, Cred: high}, {Sender: other, Cred: low}}
		v := makeProposalVote(them, rnd, 0, mid)
		o := classifyProposalOutcome(own, makeTestCert(rnd, 0, 0, them), lowestOf(rnd, 0, &v, &v))
		require.Equal(t, outcomeLostWinnerHigher, o.Kind)
		require.Equal(t, other, o.Best.Sender)
	})
}

func TestSettlePendingOutcome(t *testing.T) {
	partitiontest.PartitionTest(t)

	creds := makeSortedCreds(2)
	low, mid := creds[0], creds[1]
	us, them := makeAddr(1), makeAddr(2)
	const rnd = round(100)

	o := proposalOutcome{Round: rnd, Best: ownProposal{Sender: us, Cred: mid}}

	ours := makeProposalVote(us, rnd, 0, mid)
	reason, late := settlePendingOutcome(o, lowestOf(rnd, 0, &ours, &ours))
	require.Equal(t, lostNoBetterSeen, reason)
	require.Nil(t, late)

	better := makeProposalVote(them, rnd, 0, low)
	reason, late = settlePendingOutcome(o, lowestOf(rnd, 0, &ours, &better))
	require.Equal(t, lostBetterArrivedLate, reason)
	require.Equal(t, them, late.R.Sender)
}

func TestOwnProposalTracker(t *testing.T) {
	partitiontest.PartitionTest(t)

	var nilTracker *ownProposalTracker
	nilTracker.record(vote{})
	own, stale := nilTracker.certified(1)
	require.Nil(t, own)
	require.Nil(t, stale)

	tr := makeOwnProposalTracker()
	cred := makeSortedCreds(1)[0]
	tr.record(makeProposalVote(makeAddr(1), 5, 0, cred))
	tr.record(makeProposalVote(makeAddr(2), 7, 0, cred))
	tr.record(makeProposalVote(makeAddr(1), 7, 1, cred))
	tr.record(makeProposalVote(makeAddr(1), 8, 0, cred))

	own, stale = tr.certified(7)
	require.Len(t, own, 2)
	require.Len(t, stale, 1)
	require.Len(t, stale[5], 1)
	require.Len(t, tr.own, 1)
	require.Contains(t, tr.own, round(8))

	tr.addPending(proposalOutcome{Round: 3})
	tr.addPending(proposalOutcome{Round: 4})
	tr.addPending(proposalOutcome{Round: 9})
	_, ok := tr.takePending(4)
	require.True(t, ok)
	_, ok = tr.takePending(4)
	require.False(t, ok)
	expired := tr.expirePending(8)
	require.Len(t, expired, 1)
	require.Equal(t, round(3), expired[0].Round)
	require.Len(t, tr.pending, 1)
}

func TestReportProposalOutcome(t *testing.T) {
	partitiontest.PartitionTest(t)

	creds := makeSortedCreds(3)
	low, mid, high := creds[0], creds[1], creds[2]
	us, them, other := makeAddr(1), makeAddr(2), makeAddr(3)

	var buf bytes.Buffer
	log := logging.NewLogger()
	log.SetOutput(&buf)
	log.SetLevel(logging.Info)
	s := &Service{log: serviceLogger{log}, ownProposals: makeOwnProposalTracker()}

	warnings := func() []string {
		var out []string
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.Contains(line, "proposal outcome") && strings.Contains(line, "level=warning") {
				out = append(out, line)
			}
		}
		buf.Reset()
		return out
	}

	// round 100: we proposed with the lowest credential observed, them certified
	r1 := round(100)
	ours1 := makeProposalVote(us, r1, 0, mid)
	s.ownProposals.record(ours1)
	s.reportProposalOutcome(makeTestCert(r1, 0, 0, them), lowestOf(r1, 0, &ours1, &ours1), readLowestEvent{}, 0)
	require.Empty(t, warnings(), "decision waits for late credentials")
	require.Contains(t, s.ownProposals.pending, r1)

	// round 101: same, but no better credential ever arrives
	r2 := r1 + 1
	ours2 := makeProposalVote(us, r2, 0, mid)
	s.ownProposals.record(ours2)
	s.reportProposalOutcome(makeTestCert(r2, 0, 0, them), lowestOf(r2, 0, &ours2, &ours2), readLowestEvent{}, 0)

	// round 102: winner credential known to be higher than ours, warn right away
	r3 := r2 + 1
	winner := makeProposalVote(them, r3, 0, high)
	ours3 := makeProposalVote(us, r3, 0, mid)
	s.ownProposals.record(ours3)
	s.reportProposalOutcome(makeTestCert(r3, 0, 0, them), lowestOf(r3, 0, &winner, &ours3), readLowestEvent{}, 0)
	w := warnings()
	require.Len(t, w, 1)
	require.Contains(t, w[0], string(lostWinnerHigher))

	// round 100+lag: lagged read for round 100 shows a better credential that arrived late
	late := makeProposalVote(other, r1, 0, low)
	s.reportProposalOutcome(makeTestCert(r1+credentialRoundLag, 0, 0, them), readLowestEvent{}, lowestOf(r1, 0, &ours1, &late), 0)
	w = warnings()
	require.Len(t, w, 1)
	require.Contains(t, w[0], string(lostBetterArrivedLate))
	require.Contains(t, w[0], "LateCheck=true")

	// round 101+lag: lagged read not available (e.g. not in period 0), expire with a warning
	s.reportProposalOutcome(makeTestCert(r2+credentialRoundLag, 0, 0, them), readLowestEvent{}, readLowestEvent{}, 0)
	w = warnings()
	require.Len(t, w, 1)
	require.Contains(t, w[0], string(lostNoBetterSeen))
	require.Contains(t, w[0], "LateCheck=false")
	require.Empty(t, s.ownProposals.pending)

	// won and outbid rounds log at info only
	r4 := round(200)
	ours4 := makeProposalVote(us, r4, 0, low)
	s.ownProposals.record(ours4)
	s.reportProposalOutcome(makeTestCert(r4, 0, 0, us), lowestOf(r4, 0, &ours4, &ours4), readLowestEvent{}, 0)
	require.Empty(t, warnings())

	// nil tracker is a no-op
	s.ownProposals = nil
	s.reportProposalOutcome(makeTestCert(r4+1, 0, 0, us), readLowestEvent{}, readLowestEvent{}, 0)
	require.Empty(t, warnings())
}
