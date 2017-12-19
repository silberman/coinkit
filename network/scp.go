package network

import (
	"fmt"
	"log"
	"time"
)

// Stuff for implementing the Stellar Consensus Protocol. See:
// https://www.stellar.org/papers/stellar-consensus-protocol.pdf
// When there are frustrating single-letter variable names, it's because we are
// making the names line up with the protocol paper.

type NominationMessage struct {
	// What slot we are nominating values for
	I int

	// The values we have voted to nominate
	X []SlotValue

	// The values we have accepted as nominated
	Y []SlotValue
	
	D QuorumSlice
}

func (m *NominationMessage) MessageType() string {
	return "Nomination"
}

// See page 21 of the protocol paper for more detail here.
type NominationState struct {
	// The values we have voted to nominate
	X []SlotValue

	// The values we have accepted as nominated
	Y []SlotValue

	// The values that we consider to be candidates 
	Z []SlotValue

	// The last NominationMessage received from each node
	N map[string]*NominationMessage

	// Who we are
	publicKey string

	// Who we listen to for quorum
	D QuorumSlice
}

func NewNominationState(publicKey string, qs QuorumSlice) *NominationState {
	return &NominationState{
		X: make([]SlotValue, 0),
		Y: make([]SlotValue, 0),
		Z: make([]SlotValue, 0),
		N: make(map[string]*NominationMessage),
		publicKey: publicKey,
		D: qs,
	}
}

// HasNomination tells you whether this nomination state can currently send out
// a nominate message.
// If we have never received a nomination from a peer, and haven't had SetDefault
// called ourselves, then we won't have a nomination.
func (s *NominationState) HasNomination() bool {
	return len(s.X) > 0
}

func (s *NominationState) SetDefault(v SlotValue) {
	if s.HasNomination() {
		// We already have something to nominate
		return
	}
	s.X = []SlotValue{v}
}

// PredictValue can predict the value iff HasNomination is true. If not, panic
func (s *NominationState) PredictValue() SlotValue {
	if len(s.Z) > 0 {
		return CombineSlice(s.Z)
	}
	if len(s.Y) > 0 {
		return CombineSlice(s.Y)
	}
	if len(s.X) > 0 {
		return CombineSlice(s.X)
	}
	panic("PredictValue was called when HasNomination was false")
}

func (s *NominationState) QuorumSlice(node string) (*QuorumSlice, bool) {
	if node == s.publicKey {
		return &s.D, true
	}
	m, ok := s.N[node]
	if !ok {
		return nil, false
	}
	return &m.D, true
}

func (s *NominationState) PublicKey() string {
	return s.publicKey
}

// MaybeAdvance checks whether we should accept the nomination for this slot value,
// and adds it to our accepted list if appropriate.
// It also checks whether we should confirm the nomination.
// Returns whether we made any changes.
func (s *NominationState) MaybeAdvance(v SlotValue) bool {
	if HasSlotValue(s.Z, v) {
		// We already confirmed this, so we can't do anything more
		return false
	}
	
	changed := false	
	votedOrAccepted := []string{}
	accepted := []string{}
	if HasSlotValue(s.X, v) {
		votedOrAccepted = append(votedOrAccepted, s.publicKey)
	}
	if HasSlotValue(s.Y, v) {
		accepted = append(accepted, s.publicKey)
	}
	for node, m := range s.N {
		if HasSlotValue(m.Y, v) {
			votedOrAccepted = append(votedOrAccepted, node)
			accepted = append(accepted, node)
			continue
		}
		if HasSlotValue(m.X, v) {
			votedOrAccepted = append(votedOrAccepted, node)
		}
	}

	// The rules for accepting are on page 13, section 5.3
	// Rule 1: if a quorum has either voted for the nomination or accepted the
	// nomination, we accept it.
	// Rule 2: if a blocking set for us accepts the nomination, we accept it.
	accept := MeetsQuorum(s, votedOrAccepted) || s.D.BlockedBy(accepted)

	if accept && !HasSlotValue(s.Y, v) {
		// Accept this value
		log.Printf("I accept the nomination of %+v", v)
		changed = true
		s.Y = append(s.Y, v)
	}

	// We confirm once a quorum has accepted
	if MeetsQuorum(s, accepted) {
		log.Printf("I confirm the nomination of %+v", v)		
		changed = true
		s.Z = append(s.Z, v)
	}
	return changed
}

// Handles an incoming nomination message from a peer node
func (s *NominationState) Handle(node string, m *NominationMessage) {
	// What nodes we have seen new information about
	touched := []SlotValue{}

	// Check if there's anything new
	old, ok := s.N[node]
	var oldLenX, oldLenY int
	if ok {
		oldLenX = len(old.X)
		oldLenY = len(old.Y)
	}
	if len(m.X) < oldLenX {
		log.Printf("node %s sent a stale message: %v", node, m)
		return
	}
	if len(m.Y) < oldLenY {
		log.Printf("node %s sent a stale message: %v", node, m)
		return
	}
	if len(m.X) == oldLenX && len(m.Y) == oldLenY {
		// It's just a dupe
		return
	}
	// Update our most-recent-message
	s.N[node] = m
	
	for i := oldLenX; i < len(m.X); i++ {
		if !HasSlotValue(touched, m.X[i]) {
			touched = append(touched, m.X[i])
		}

		// If we don't have a candidate, we can support this new nomination
		if !HasSlotValue(s.X, m.X[i]) {
			log.Printf("I support the nomination of %+v", m.X[i])
			s.X = append(s.X, m.X[i])
		}
	}

	for i := oldLenY; i < len(m.Y); i++ {
		if !HasSlotValue(touched, m.Y[i]) {
			touched = append(touched, m.Y[i])
		}
	}

	for _, v := range touched {
		s.MaybeAdvance(v)
	}
}

// See page 23 of the protocol paper for more detail here.
// The null ballot is represented by a nil.
type BallotState struct {
	// What phase of balloting we are in
	phase Phase
	
	// The current ballot we are trying to prepare and commit.
	b *Ballot

	// The highest two incompatible ballots that are accepted as prepared.
	// p is the highest, pPrime the next.
	// It's nil if there is no such ballot.
	p *Ballot
	pPrime *Ballot

	// [cn, hn] defines a range of ballot numbers that defines a range of
	// b-compatible ballots.
	// [0, 0] is the invalid range, rather than [0], since 0 is an invalid ballot
	// number.
	// In the Prepare phase, this is the range we have voted to commit (which
	// we do when we can confirm the ballot is prepared) but that we have not
	// aborted.
	// In the Confirm phase, this is the range we have accepted a commit.
	// In the Externalize phase, this is the range we have confirmed a commit.
	cn int
	hn int

	// The value to use in the next ballot, if this ballot fails.
	// We may have no idea what value we would use. In that case, z is nil.
	z *SlotValue
	
	// The latest PrepareMessage, ConfirmMessage, or ExternalizeMessage from
	// each peer
	M map[string]BallotMessage

	// Who we are
	publicKey string

	// Who we listen to for quorum
	D QuorumSlice
}

func NewBallotState(publicKey string, qs QuorumSlice) *BallotState {
	return &BallotState{
		phase: Prepare,
		M: make(map[string]BallotMessage),
		publicKey: publicKey,
		D: qs,
	}
}

func (s *BallotState) PublicKey() string {
	return s.publicKey
}

func (s *BallotState) QuorumSlice(node string) (*QuorumSlice, bool) {
	if node == s.publicKey {
		return &s.D, true
	}
	m, ok := s.M[node]
	if !ok {
		return nil, false
	}
	qs := m.QuorumSlice()
	return &qs, true
}

func (s *BallotState) MaybeAcceptAsPrepared(n int, x SlotValue) {
	if s.phase != Prepare {
		return
	}
	if n == 0 {
		return
	}

	// Check if we already accept this as prepared
	if s.p != nil && s.p.n >= n && Equal(s.p.x, x) {
		return
	}
	if s.pPrime != nil && s.pPrime.n >= n && Equal(s.pPrime.x, x) {
		return
	}

	if s.pPrime != nil && s.pPrime.n >= n {
		// This is about an old ballot number, we don't care even if it is
		// accepted
		return
	}
	
	// The rules for accepting are, if a quorum has voted or accepted,
	// we can accept.
	// Or, if a local blocking set has accepted, we can accept.
	votedOrAccepted := []string{}
	accepted := []string{}
	if s.b != nil && s.b.n >= n && Equal(s.b.x, x) {
		// We have voted for this
		votedOrAccepted = append(votedOrAccepted, s.publicKey)
	}

	for node, m := range s.M {
		if m.AcceptAsPrepared(n, x) {
			accepted = append(accepted, node)
			votedOrAccepted = append(votedOrAccepted, node)
			continue
		}
		if m.VoteToPrepare(n, x) {
			votedOrAccepted = append(votedOrAccepted, node)
		}
	}

	if !MeetsQuorum(s, votedOrAccepted) && !s.D.BlockedBy(accepted) {
		// We can't accept this as prepared yet
		return
	}

	if s.b != nil && s.hn <= n && !Equal(s.b.x, x) {
		// Accepting this as prepared means we have to abort b
		s.hn = 0
		s.cn = 0
		s.b = nil
	}
	
	// p and p prime should be the top two conflicting things we accept
	// as prepared. update them accordingly
	ballot := &Ballot{
		n: n,
		x: x,
	}
	
	if s.p == nil {
		s.p = ballot
	} else if Equal(s.p.x, x) {
		if n <= s.p.n {
			log.Fatal("should have short circuited already")
		}
		s.p.n = n
	} else if n >= s.p.n {
		s.pPrime = s.p
		s.p = ballot
	} else {
		// We already short circuited if it isn't worth bumping p prime
		s.pPrime = ballot
	}	
}

func (s *BallotState) MaybeConfirmAsPrepared(n int, x SlotValue) {
	if s.phase != Prepare {
		return
	}
	if s.hn >= n {
		// We are already past this ballot
		return
	}
	ballot := &Ballot{
		n: n,
		x: x,
	}
	
	// We confirm when a quorum accepts as prepared
	accepted := []string{}
	if gtecompat(s.p, ballot) || gtecompat(s.pPrime, ballot) {
		// We accept as prepared
		accepted = append(accepted, s.publicKey)
	}

	for node, m := range s.M {
		if m.AcceptAsPrepared(n, x) {
			accepted = append(accepted, node)
		}
	}

	if !MeetsQuorum(s, accepted) {
		return
	}
	
	// We can confirm this as prepared.
	// Time to vote to commit it
	if s.b != nil && !Equal(s.b.x, x) {
		// We have to abort b
		s.b = nil
		s.hn = 0
		s.cn = 0
	}

	if s.b == nil {
		// We weren't working on any ballot, but now we can work on this one
		s.b = ballot
		s.hn = n
		s.cn = n
		s.z = &x
	} else {
		// We were just working on a lower number, so bump the range
		s.hn = n
	}
}

func (s *BallotState) MaybeAcceptAsCommitted(n int, x SlotValue) {
	if s.phase == Externalize {
		return
	}
	if s.phase == Confirm && s.cn <= n && n <= s.hn {
		// We already do accept this commit
		return
	}

	votedOrAccepted := []string{}
	accepted := []string{}

	if s.phase == Prepare && s.b != nil &&
		Equal(s.b.x, x) && s.cn != 0 && s.cn <= n && n <= s.hn {
		// We vote to commit this
		votedOrAccepted = append(votedOrAccepted, s.publicKey)
	}

	for node, m := range s.M {
		if m.AcceptAsCommitted(n, x) {
			votedOrAccepted = append(votedOrAccepted, node)
			accepted = append(accepted, node)
		} else if m.VoteToCommit(n, x) {
			votedOrAccepted = append(votedOrAccepted, node)
		}
	}

	if !MeetsQuorum(s, votedOrAccepted) && !s.D.BlockedBy(accepted) {
		// We can't accept this commit yet
		return
	}

	// We accept this commit
	s.phase = Confirm
	if s.b == nil || !Equal(s.b.x, x) {
		// Totally replace our old target value
		s.b = &Ballot{
			n: n,
			x: x,
		}
		s.cn = n
		s.hn = n
		s.z = &x
	} else {
		// Just update our range of acceptance
		if n < s.cn {
			s.cn = n
		}
		if n > s.hn {
			s.hn = n
		}
	}
}

func (s *BallotState) MaybeConfirmAsCommitted(n int, x SlotValue) {
	if s.phase == Prepare {
		return
	}
	if s.b == nil || !Equal(s.b.x, x) {
		return
	}
	
	accepted := []string{}
	if s.phase == Confirm {
		if s.cn <= n && n <= s.hn {
			accepted = append(accepted, s.publicKey)
		}
	} else if s.cn <= n && n <= s.hn {
		// We already did confirm this as committed
		return
	}

	for node, m := range s.M {
		if m.AcceptAsCommitted(n, x) {
			accepted = append(accepted, node)
		}
	}

	if !MeetsQuorum(s, accepted) {
		return
	}
	if s.phase == Confirm {
		s.phase = Externalize
		s.cn = n
		s.hn = n
	} else {
		if n < s.cn {
			s.cn = n
		}
		if n > s.hn {
			s.hn = n
		}
	}
}

// Returns whether we needed to bump the ballot number.
// We bump the ballot number if the set of nodes with a higher
// ballot number is blocking.
// TODO: figure out what the distinction is between s.b.n and s.hn
// TODO: figure out if s.z and s.b.x are redundant
func (s *BallotState) MaybeNextBallot() bool {
	if s.z == nil || s.b == nil {
		return false
	}
	
	// Nodes with a higher ballot number
	higher := []string{}
	current := 0
	if s.b != nil {
		current = s.b.n
	}

	for node, m := range s.M {
		if m.BallotNumber() > current {
			higher = append(higher, node)
		}
	}

	if !s.D.BlockedBy(higher) {
		return false
	}

	// s.z and s.b.x should be equivalent here
	s.b.n++
	return true
}

// Update the stage of this ballot as needed
// See the handling algorithm on page 24 of the Mazieres paper.
// The investigate method does steps 1-8
func (s *BallotState) Investigate(n int, x SlotValue) {
	s.MaybeAcceptAsPrepared(n, x)
	s.MaybeConfirmAsPrepared(n, x)
	s.MaybeAcceptAsCommitted(n, x)
	s.MaybeConfirmAsCommitted(n, x)
}

func (s *BallotState) Handle(node string, message BallotMessage) {
	// If this message isn't new, skip it
	old, ok := s.M[node]
	if ok && Compare(old, message) >= 0 {
		return
	}
	s.M[node] = message

	for {
		// Investigate all ballots whose state might be updated
		// TODO: make sure we aren't missing ballot numbers internal to the
		// ranges
		switch m := message.(type) {
		case *PrepareMessage:
			s.Investigate(m.Bn, m.Bx)
			s.Investigate(m.Pn, m.Px)
			s.Investigate(m.Ppn, m.Ppx)
		case *ConfirmMessage:
			s.Investigate(m.Hn, m.X)
		case *ExternalizeMessage:
			for i := m.Cn; i <= m.Hn; i++ {
				s.Investigate(i, m.X)
			}
		}

		// Step 9 of the processing algorithm
		if !s.MaybeNextBallot() {
			break
		}
	}
}

func (s *BallotState) HasMessage() bool {
	return s.b != nil
}

func (s *BallotState) Message(slot int, qs QuorumSlice) Message {
	if !s.HasMessage() {
		panic("coding error")
	}

	switch s.phase {
	case Prepare:
		m := &PrepareMessage{
			T: Prepare,
			I: slot,
			Bn: s.b.n,
			Bx: s.b.x,
			Cn: s.cn,
			Hn: s.hn,
			D: qs,
		}
		if s.p != nil {
			m.Pn = s.p.n
			m.Px = s.p.x
		}
		if s.pPrime != nil {
			m.Ppn = s.pPrime.n
			m.Ppx = s.pPrime.x
		}
		return m

	case Confirm:
		m := &ConfirmMessage{
			T: Confirm,
			I: slot,
			X: s.b.x,
			Cn: s.cn,
			Hn: s.hn,
			D: qs,
		}
		if s.p != nil {
			m.Pn = s.p.n
		}
		return m

	case Externalize:
		return &ExternalizeMessage{
			T: Externalize,
			I: slot,
			X: s.b.x,
			Cn: s.cn,
			Hn: s.hn,
			D: qs,
		}
	}

	panic("code flow should not get here")
}

type StateBuilder struct {
	// Which slot is actively being built
	slot int

	// The time we started working on this slot
	start time.Time
	
	// Values for past slots that have already achieved consensus
	values map[int]SlotValue

	nState *NominationState
	bState *BallotState

	// Who we care about
	D QuorumSlice

	// Who we are
	publicKey string
}

func NewStateBuilder(publicKey string, members []string, threshold int) *StateBuilder {
	qs := QuorumSlice{
		Members: members,
		Threshold: threshold,
	}
	return &StateBuilder{
		slot: 1,
		start: time.Now(),
		values: make(map[int]SlotValue),
		nState: NewNominationState(publicKey, qs),
		D: qs,
		publicKey: publicKey,
	}
}

// OutgoingMessages returns the outgoing messages.
// There can be zero or one nomination messages, and zero or one ballot messages.
func (sb *StateBuilder) OutgoingMessages() []Message {
	answer := []Message{}

	if !sb.nState.HasNomination() {
		// There's nothing to nominate. Let's nominate something.
		// TODO: if it's not our turn, wait instead of nominating
		comment := fmt.Sprintf(
			"this is %s at %s", sb.publicKey, time.Now().Format("15:04:05.00000"))
		v := MakeSlotValue(comment)
		log.Printf("I nominate %+v", v)
		sb.nState.SetDefault(v)
	}

	answer = append(answer, &NominationMessage{
		I: sb.slot,
		X: sb.nState.X,
		Y: sb.nState.Y,
		D: sb.D,
	})

	if sb.bState.HasMessage() {
		answer = append(answer, sb.bState.Message(sb.slot, sb.D))
	}

	return answer
}

// Handle handles an incoming message
func (sb *StateBuilder) Handle(sender string, message Message) {
	switch m := message.(type) {
	case *NominationMessage:
		sb.nState.Handle(sender, m)
	case *PrepareMessage:
		sb.bState.Handle(sender, m)
	case *ConfirmMessage:
		sb.bState.Handle(sender, m)
	case *ExternalizeMessage:
		sb.bState.Handle(sender, m)
	default:
		log.Printf("unrecognized message: %v", m)
	}
}

