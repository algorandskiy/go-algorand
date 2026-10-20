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
	"encoding/hex"
	"slices"
	"time"

	"github.com/algorand/go-deadlock"

	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/committee"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/util/metrics"
)

var ownProposalRoundsCounter = metrics.MakeCounter(
	metrics.MetricName{Name: "algod_agreement_own_proposal_rounds", Description: "Number of certified rounds in which this node sent a proposal"})

var ownProposalWonCounter = metrics.MakeCounter(
	metrics.MetricName{Name: "algod_agreement_own_proposal_won", Description: "Number of certified rounds in which this node's proposal was certified"})

var ownProposalLostByType = metrics.NewTagCounter("algod_agreement_own_proposal_lost_{TAG}",
	"Number of rounds in which this node's proposal had a lower credential than any observed but was not certified ({TAG})",
	string(lostBetterArrivedLate), string(lostNoBetterSeen), string(lostWinnerHigher))

// lostReason tags a round in which our proposal had the lowest credential
// known to us, yet another proposal was certified.
//
//msgp:ignore lostReason
type lostReason string

const (
	// lostBetterArrivedLate: a lower credential reached us only after certification.
	lostBetterArrivedLate lostReason = "better_arrived_late"
	// lostNoBetterSeen: no lower credential reached us at all.
	lostNoBetterSeen lostReason = "no_better_seen"
	// lostWinnerHigher: the certified proposal's credential is known to be higher than ours.
	lostWinnerHigher lostReason = "winner_higher"
)

// proposalOutcomeKind is the result of comparing our proposals for a round
// against the certificate for that round.
//
//msgp:ignore proposalOutcomeKind
type proposalOutcomeKind int

const (
	outcomeNone proposalOutcomeKind = iota
	// outcomeWon: one of our proposals was certified.
	outcomeWon
	// outcomeOutbid: the certified proposal's credential is known and lower than ours.
	outcomeOutbid
	// outcomeNotLowest: some other credential was lower than ours, and the
	// certified proposal's credential is unknown.
	outcomeNotLowest
	// outcomeDifferentPeriod: we did not propose in the period the certified value was proposed in.
	outcomeDifferentPeriod
	// outcomeLostWinnerHigher: our credential is lower than the certified proposal's.
	outcomeLostWinnerHigher
	// outcomePending: ours is the lowest credential observed, but another proposal
	// was certified. The decision waits credentialRoundLag rounds for late credentials.
	outcomePending
)

// ownProposal is a proposal-vote this node created and verified.
//
//msgp:ignore ownProposal
type ownProposal struct {
	Sender basics.Address
	Period period
	Cred   committee.Credential
}

// proposalOutcome holds what is known about our proposals once a round is certified.
//
//msgp:ignore proposalOutcome
type proposalOutcome struct {
	Kind   proposalOutcomeKind
	Round  round
	Cert   Certificate
	Own    []ownProposal
	Best   ownProposal // our lowest-credential proposal in Cert.Proposal.OriginalPeriod
	Lowest readLowestEvent

	// WinnerCred is the certified proposal's credential, when it reached us.
	WinnerCred    committee.Credential
	HasWinnerCred bool

	DynamicFilterTimeout time.Duration
}

// ownProposalTracker remembers the proposals this node made until their rounds
// are certified, and the suspicious outcomes until late credentials can be ruled in or out.
//
//msgp:ignore ownProposalTracker
type ownProposalTracker struct {
	mu      deadlock.Mutex
	own     map[round][]ownProposal
	pending map[round]proposalOutcome
}

func makeOwnProposalTracker() *ownProposalTracker {
	return &ownProposalTracker{
		own:     make(map[round][]ownProposal),
		pending: make(map[round]proposalOutcome),
	}
}

// record remembers a proposal-vote created by this node.
func (t *ownProposalTracker) record(v vote) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.own[v.R.Round] = append(t.own[v.R.Round], ownProposal{Sender: v.R.Sender, Period: v.R.Period, Cred: v.Cred})
}

// certified returns and forgets our proposals for rnd. Proposals for older
// rounds are returned in stale: those rounds were never certified here
// (e.g., the node caught up through the ledger).
func (t *ownProposalTracker) certified(rnd round) (own []ownProposal, stale map[round][]ownProposal) {
	if t == nil {
		return nil, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	own = t.own[rnd]
	delete(t.own, rnd)
	for r, props := range t.own {
		if r < rnd {
			if stale == nil {
				stale = make(map[round][]ownProposal)
			}
			stale[r] = props
			delete(t.own, r)
		}
	}
	return own, stale
}

func (t *ownProposalTracker) addPending(o proposalOutcome) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[o.Round] = o
}

// takePending returns and forgets the pending outcome for rnd, if any.
func (t *ownProposalTracker) takePending(rnd round) (proposalOutcome, bool) {
	if t == nil {
		return proposalOutcome{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	o, ok := t.pending[rnd]
	delete(t.pending, rnd)
	return o, ok
}

// expirePending returns and forgets pending outcomes for rounds at or before rnd.
func (t *ownProposalTracker) expirePending(rnd round) (expired []proposalOutcome) {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for r, o := range t.pending {
		if r <= rnd {
			expired = append(expired, o)
			delete(t.pending, r)
		}
	}
	slices.SortFunc(expired, func(a, b proposalOutcome) int { return int(a.Round) - int(b.Round) })
	return expired
}

// classifyProposalOutcome compares our proposals for a round against its
// certificate and the lowest proposal-votes observed for the certified value's
// original period.
func classifyProposalOutcome(own []ownProposal, cert Certificate, lowest readLowestEvent) proposalOutcome {
	o := proposalOutcome{Kind: outcomeNone, Round: cert.Round, Cert: cert, Own: own, Lowest: lowest}
	if len(own) == 0 {
		return o
	}

	winner := cert.Proposal.OriginalProposer
	hasBest := false
	for _, p := range own {
		if p.Sender == winner {
			o.Kind = outcomeWon
		}
		if p.Period == cert.Proposal.OriginalPeriod && (!hasBest || p.Cred.Less(o.Best.Cred)) {
			o.Best = p
			hasBest = true
		}
	}
	if o.Kind == outcomeWon {
		return o
	}
	if !hasBest {
		o.Kind = outcomeDifferentPeriod
		return o
	}

	if lowest.HasLowestIncludingLate && lowest.LowestIncludingLate.R.Sender == winner {
		o.WinnerCred, o.HasWinnerCred = lowest.LowestIncludingLate.Cred, true
	} else if lowest.Filled && lowest.Vote.R.Sender == winner {
		o.WinnerCred, o.HasWinnerCred = lowest.Vote.Cred, true
	}
	if o.HasWinnerCred {
		if o.Best.Cred.Less(o.WinnerCred) {
			o.Kind = outcomeLostWinnerHigher
		} else {
			o.Kind = outcomeOutbid
		}
		return o
	}

	if lowest.HasLowestIncludingLate && lowest.LowestIncludingLate.Cred.Less(o.Best.Cred) {
		o.Kind = outcomeNotLowest
		return o
	}
	o.Kind = outcomePending
	return o
}

// settlePendingOutcome decides a pending outcome given the lowest
// proposal-vote observed for its round credentialRoundLag rounds later.
func settlePendingOutcome(o proposalOutcome, lagged readLowestEvent) (lostReason, *vote) {
	if lagged.HasLowestIncludingLate && lagged.LowestIncludingLate.Cred.Less(o.Best.Cred) {
		return lostBetterArrivedLate, &lagged.LowestIncludingLate
	}
	return lostNoBetterSeen, nil
}

// credPrefix renders the leading bytes of a credential's lowest output, which
// is the value proposals are ordered by: a smaller value wins.
func credPrefix(c committee.Credential) string {
	if !c.Selected() {
		return ""
	}
	d := c.LowestOutputDigest()
	return hex.EncodeToString(d[:8])
}

func (o proposalOutcome) fields() logging.Fields {
	f := logging.Fields{
		"Round":                o.Round,
		"CertPeriod":           o.Cert.Period,
		"OriginalPeriod":       o.Cert.Proposal.OriginalPeriod,
		"Winner":               o.Cert.Proposal.OriginalProposer.String(),
		"OurSender":            o.Best.Sender.String(),
		"OurWeight":            o.Best.Cred.Weight,
		"OurCred":              credPrefix(o.Best.Cred),
		"DynamicFilterTimeout": o.DynamicFilterTimeout.String(),
	}
	if o.HasWinnerCred {
		f["WinnerWeight"] = o.WinnerCred.Weight
		f["WinnerCred"] = credPrefix(o.WinnerCred)
	}
	if o.Lowest.HasLowestIncludingLate {
		lv := o.Lowest.LowestIncludingLate
		f["LowestSender"] = lv.R.Sender.String()
		f["LowestWeight"] = lv.Cred.Weight
		f["LowestCred"] = credPrefix(lv.Cred)
		f["LowestValidatedAt"] = lv.validatedAt.String()
	}
	// whether our proposal-vote was the lowest one when our own filter froze; if it is
	// the lowest observed but this is false, it reached our own state machine late
	f["OurLowestBeforeFreeze"] = o.Lowest.Filled && o.Lowest.Vote.R.Sender == o.Best.Sender && o.Lowest.Vote.Cred.Equals(o.Best.Cred)
	if o.Lowest.HasLowestIncludingLate && o.Lowest.LowestIncludingLate.R.Sender == o.Best.Sender {
		f["OurValidatedAt"] = o.Lowest.LowestIncludingLate.validatedAt.String()
	}
	return f
}

// reportProposalOutcome compares the proposals this node made for the
// certified round with the certificate, and logs the result. It also settles
// the suspicious outcome (if any) of the round credentialRoundLag rounds ago,
// using laggedLowest, which includes proposal-votes that arrived late.
func (s *Service) reportProposalOutcome(cert Certificate, lowest readLowestEvent, laggedLowest readLowestEvent, dynamicFilterTimeout time.Duration) {
	t := s.ownProposals
	if t == nil {
		return
	}

	if laggedLowest.T == readLowestVote { // set only if the player read it
		if o, ok := t.takePending(laggedLowest.Round); ok {
			reason, late := settlePendingOutcome(o, laggedLowest)
			s.logLost(o, reason, late, true)
		}
	}
	// pending outcomes whose late check could not be done (e.g., the player was
	// not in period 0 when credentialRoundLag rounds had passed)
	if cert.Round > credentialRoundLag {
		for _, o := range t.expirePending(cert.Round - credentialRoundLag) {
			s.logLost(o, lostNoBetterSeen, nil, false)
		}
	}

	own, stale := t.certified(cert.Round)
	for r, props := range stale {
		senders := make([]string, len(props))
		for i := range props {
			senders[i] = props[i].Sender.String()
		}
		s.log.WithFields(logging.Fields{"Round": r, "OurSenders": senders}).
			Infof("proposal outcome: round %d concluded without a certificate observed by agreement; outcome of our proposal unknown", r)
	}
	if len(own) == 0 {
		return
	}

	o := classifyProposalOutcome(own, cert, lowest)
	o.DynamicFilterTimeout = dynamicFilterTimeout
	ownProposalRoundsCounter.Inc(nil)
	log := s.log.WithFields(o.fields())
	switch o.Kind {
	case outcomeWon:
		ownProposalWonCounter.Inc(nil)
		log.Infof("proposal outcome: round %d: our proposal from %v was certified", o.Round, o.Cert.Proposal.OriginalProposer)
	case outcomeOutbid:
		log.Infof("proposal outcome: round %d: outbid by %v with a lower credential", o.Round, o.Cert.Proposal.OriginalProposer)
	case outcomeNotLowest:
		log.Infof("proposal outcome: round %d: %v had a lower credential than ours; certified %v", o.Round, o.Lowest.LowestIncludingLate.R.Sender, o.Cert.Proposal.OriginalProposer)
	case outcomeDifferentPeriod:
		periods := make([]period, len(own))
		for i := range own {
			periods[i] = own[i].Period
		}
		log.WithFields(logging.Fields{"OurPeriods": periods}).
			Infof("proposal outcome: round %d: certified %v from period %d; we proposed in other periods", o.Round, o.Cert.Proposal.OriginalProposer, o.Cert.Proposal.OriginalPeriod)
	case outcomeLostWinnerHigher:
		s.logLost(o, lostWinnerHigher, nil, false)
	case outcomePending:
		if o.Cert.Proposal.OriginalPeriod != 0 {
			// late credentials are only tracked for period 0
			s.logLost(o, lostNoBetterSeen, nil, false)
		} else {
			t.addPending(o)
		}
	}
}

// logLost reports a round in which our proposal had the lowest credential
// known to us but another proposal was certified.
func (s *Service) logLost(o proposalOutcome, reason lostReason, late *vote, lateChecked bool) {
	ownProposalLostByType.Add(string(reason), 1)
	f := o.fields()
	f["LostReason"] = string(reason)
	f["LateCheck"] = lateChecked
	if late != nil {
		f["LateSender"] = late.R.Sender.String()
		f["LateWeight"] = late.Cred.Weight
		f["LateCred"] = credPrefix(late.Cred)
		f["LateValidatedAt"] = late.validatedAt.String()
		f["LateIsWinner"] = late.R.Sender == o.Cert.Proposal.OriginalProposer
	}
	log := s.log.WithFields(f)
	switch reason {
	case lostWinnerHigher:
		log.Warnf("proposal outcome: round %d: our proposal from %v had a lower credential than certified %v; our proposal was late or dropped at other validators",
			o.Round, o.Best.Sender, o.Cert.Proposal.OriginalProposer)
	case lostBetterArrivedLate:
		log.Warnf("proposal outcome: round %d: our proposal from %v was not certified (certified %v); a lower credential from %v reached us only after certification",
			o.Round, o.Best.Sender, o.Cert.Proposal.OriginalProposer, late.R.Sender)
	default:
		log.Warnf("proposal outcome: round %d: our proposal from %v had the lowest credential observed but %v was certified; our proposal was likely late or dropped at other validators, or a better one never reached us",
			o.Round, o.Best.Sender, o.Cert.Proposal.OriginalProposer)
	}
}
