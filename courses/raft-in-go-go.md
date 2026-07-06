---
course: raft-in-go
title: Raft in Go
language: go
description: Implement the Raft consensus algorithm piece by piece as pure, deterministic functions — elections, log replication, commitment rules, and a full in-memory cluster simulation, with the paper as your spec.
duration_hours: 14
tags: [distributed-systems, consensus, raft, advanced]
extended_reading:
  - title: "In Search of an Understandable Consensus Algorithm (the Raft paper)"
    url: https://raft.github.io/raft.pdf
  - title: "The Raft visualization"
    url: https://raft.github.io/
  - title: "Consensus: Bridging Theory and Practice (Ongaro's dissertation)"
    url: https://github.com/ongardie/dissertation
---

# Lesson: The Problem and the Players {#the-problem-and-the-players}

A single server that applies commands in order is easy to reason about: it is a
**state machine**. Feed it the same commands in the same order and it always
ends up in the same state. The whole trick of fault-tolerant systems is to run
*several* copies of that state machine and keep them in sync — a **replicated
state machine**. If every replica applies the same log of commands in the same
order, the replicas are interchangeable, and the service survives the loss of a
minority of them.

So the real problem is not replicating the state machine. It is replicating the
**log**. That is what a consensus algorithm does, and Raft (Ongaro &
Ousterhout, *In Search of an Understandable Consensus Algorithm*) is a
consensus algorithm designed above all to be understandable. Keep the paper
open while you take this course — **Figure 2 of the paper is the complete
specification**, and every challenge here implements a piece of it verbatim.
Struct and field names in this course deliberately match the paper
(`currentTerm`, `votedFor`, `prevLogIndex`, `leaderCommit`, …) so the paper
reads as your documentation.

### Terms: Raft's logical clock

Raft divides time into **terms**, numbered with consecutive integers. Each term
begins with an election; at most one leader can win a given term. Terms act as
a logical clock: every message carries the sender's term, and servers use it to
detect stale information. Two rules from Figure 2 ("Rules for Servers, All
Servers") do most of the work:

- If a request **or response** contains a term `T > currentTerm`: set
  `currentTerm = T` and convert to follower.
- If a message carries a term *smaller* than `currentTerm`, it is stale: reject
  or ignore it (replying with your own term so the sender can update itself).

### The three states

At any moment a server is in exactly one of three states (paper §5.1):

- **Follower** — passive; responds to requests from leaders and candidates.
- **Candidate** — trying to get elected leader.
- **Leader** — handles all client requests and drives log replication.

Figure 4 of the paper draws the transitions:

```go
// Follower  --times out, starts election-->            Candidate
// Candidate --times out, new election-->               Candidate (again)
// Candidate --receives votes from majority-->          Leader
// Candidate --discovers current leader or new term---> Follower
// Leader    --discovers server with higher term-->     Follower
```

A follower that hears nothing from a leader for an *election timeout* assumes
the leader is dead: it increments `currentTerm`, votes for itself, and becomes
a candidate. A candidate that gathers votes from a **majority** of the cluster
becomes leader. A candidate that instead receives an AppendEntries request from
a legitimate leader (same or higher term) steps back down to follower. And
*anyone* who sees a higher term — leader included — immediately becomes a
follower of that term.

Majorities are why Raft works: any two majorities of the same cluster overlap
in at least one server, so at most one candidate can win a term, and any
elected leader has talked to at least one server that saw previous decisions.

### Modeling this deterministically

In this course we never open a socket or start a timer. Time and messages are
**data**: an election timeout is just an event value, an incoming RPC is just a
struct. Every function you write is a pure, deterministic step over in-memory
state — which is exactly how you want to unit-test a consensus algorithm before
you ever wire it to a network.

## Challenge: The State Transition Function {#step-function points=25}

Implement `Step`, the Figure 4 transition function. It takes a server's
election-relevant state and one event, and returns the new state (it must not
matter that the input is passed by value — return the updated copy).

Events model the three stimuli a server's election logic reacts to:

- `EventTimeout` — the election timer fired. `Term` is 0 for this event.
- `EventVote` — a RequestVote **reply** arrived, carrying the replier's `Term`
  and whether the vote was `Granted`.
- `EventAppend` — an AppendEntries request arrived from a server claiming to be
  leader, carrying its `Term`.

Apply these rules, in this order:

1. **Higher term wins** (Figure 2, All Servers): if the event is a message
   (`EventVote` or `EventAppend`) and `ev.Term > s.CurrentTerm`, set
   `CurrentTerm = ev.Term`, become `Follower`, reset `VotedFor` to `-1` and
   `VotesGranted` to 0 — then continue processing the event.
2. **`EventTimeout`**: leaders ignore it (a leader has no election timer).
   Followers and candidates become `Candidate`: increment `CurrentTerm`, set
   `VotedFor = s.ID`, set `VotesGranted = 1` (you vote for yourself). If that
   single vote is already a majority (`2*VotesGranted > ClusterSize` — a
   one-node cluster), become `Leader` immediately.
3. **`EventVote`**: only a candidate counts votes, and only replies that are
   `Granted` **and** carry exactly the current term (stale-term replies are
   ignored). Increment `VotesGranted`; on reaching a majority, become `Leader`.
4. **`EventAppend`**: if `ev.Term < s.CurrentTerm`, ignore it — the sender is a
   stale leader. Otherwise the sender is the legitimate leader of the current
   term: a candidate steps down to `Follower`; a follower stays a follower; the
   term and `VotedFor` are unchanged.

### Starter

```go
package challenge

// Role is one of the three server states from §5.1 of the Raft paper.
type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

// EventType classifies the stimuli the election logic reacts to.
type EventType int

const (
	EventTimeout EventType = iota // the election timer fired
	EventVote                     // a RequestVote reply arrived
	EventAppend                   // an AppendEntries request arrived
)

// Event is one input to the state machine. Term is the term carried by the
// message (0 for EventTimeout). Granted is meaningful only for EventVote.
type Event struct {
	Type    EventType
	Term    int
	Granted bool
}

// State is the election-relevant state of one server.
type State struct {
	ID           int  // this server's ID
	Role         Role
	CurrentTerm  int
	VotedFor     int // ID voted for in CurrentTerm; -1 = none
	VotesGranted int // votes received in CurrentTerm (as a candidate)
	ClusterSize  int // total number of servers in the cluster
}

// Step applies one event and returns the resulting state.
func Step(s State, ev Event) State {
	// TODO: implement the Figure 4 transitions.
	return s
}
```

### Tests

```go
package challenge

import "testing"

func TestStep(t *testing.T) {
	cases := []struct {
		name string
		in   State
		ev   Event
		want State
	}{
		{
			"follower election timeout starts an election",
			State{ID: 1, Role: Follower, CurrentTerm: 2, VotedFor: -1, ClusterSize: 3},
			Event{Type: EventTimeout},
			State{ID: 1, Role: Candidate, CurrentTerm: 3, VotedFor: 1, VotesGranted: 1, ClusterSize: 3},
		},
		{
			"candidate retries after a split vote",
			State{ID: 1, Role: Candidate, CurrentTerm: 3, VotedFor: 1, VotesGranted: 1, ClusterSize: 3},
			Event{Type: EventTimeout},
			State{ID: 1, Role: Candidate, CurrentTerm: 4, VotedFor: 1, VotesGranted: 1, ClusterSize: 3},
		},
		{
			"leader ignores election timeouts",
			State{ID: 1, Role: Leader, CurrentTerm: 3, VotedFor: 1, VotesGranted: 2, ClusterSize: 3},
			Event{Type: EventTimeout},
			State{ID: 1, Role: Leader, CurrentTerm: 3, VotedFor: 1, VotesGranted: 2, ClusterSize: 3},
		},
		{
			"candidate collects a vote, five nodes, not yet a majority",
			State{ID: 2, Role: Candidate, CurrentTerm: 5, VotedFor: 2, VotesGranted: 1, ClusterSize: 5},
			Event{Type: EventVote, Term: 5, Granted: true},
			State{ID: 2, Role: Candidate, CurrentTerm: 5, VotedFor: 2, VotesGranted: 2, ClusterSize: 5},
		},
		{
			"third vote of five wins the election",
			State{ID: 2, Role: Candidate, CurrentTerm: 5, VotedFor: 2, VotesGranted: 2, ClusterSize: 5},
			Event{Type: EventVote, Term: 5, Granted: true},
			State{ID: 2, Role: Leader, CurrentTerm: 5, VotedFor: 2, VotesGranted: 3, ClusterSize: 5},
		},
		{
			"second vote of three wins the election",
			State{ID: 0, Role: Candidate, CurrentTerm: 1, VotedFor: 0, VotesGranted: 1, ClusterSize: 3},
			Event{Type: EventVote, Term: 1, Granted: true},
			State{ID: 0, Role: Leader, CurrentTerm: 1, VotedFor: 0, VotesGranted: 2, ClusterSize: 3},
		},
		{
			"stale-term vote reply is ignored",
			State{ID: 2, Role: Candidate, CurrentTerm: 5, VotedFor: 2, VotesGranted: 1, ClusterSize: 5},
			Event{Type: EventVote, Term: 4, Granted: true},
			State{ID: 2, Role: Candidate, CurrentTerm: 5, VotedFor: 2, VotesGranted: 1, ClusterSize: 5},
		},
		{
			"rejection carrying a higher term dethrones the candidate",
			State{ID: 2, Role: Candidate, CurrentTerm: 5, VotedFor: 2, VotesGranted: 2, ClusterSize: 5},
			Event{Type: EventVote, Term: 7, Granted: false},
			State{ID: 2, Role: Follower, CurrentTerm: 7, VotedFor: -1, VotesGranted: 0, ClusterSize: 5},
		},
		{
			"follower ignores vote replies",
			State{ID: 1, Role: Follower, CurrentTerm: 5, VotedFor: -1, ClusterSize: 3},
			Event{Type: EventVote, Term: 5, Granted: true},
			State{ID: 1, Role: Follower, CurrentTerm: 5, VotedFor: -1, ClusterSize: 3},
		},
		{
			"candidate steps down to a same-term leader",
			State{ID: 1, Role: Candidate, CurrentTerm: 4, VotedFor: 1, VotesGranted: 1, ClusterSize: 3},
			Event{Type: EventAppend, Term: 4},
			State{ID: 1, Role: Follower, CurrentTerm: 4, VotedFor: 1, VotesGranted: 1, ClusterSize: 3},
		},
		{
			"candidate ignores a stale leader",
			State{ID: 1, Role: Candidate, CurrentTerm: 4, VotedFor: 1, VotesGranted: 1, ClusterSize: 3},
			Event{Type: EventAppend, Term: 3},
			State{ID: 1, Role: Candidate, CurrentTerm: 4, VotedFor: 1, VotesGranted: 1, ClusterSize: 3},
		},
		{
			"follower adopts a higher term from an append",
			State{ID: 2, Role: Follower, CurrentTerm: 4, VotedFor: 1, ClusterSize: 3},
			Event{Type: EventAppend, Term: 6},
			State{ID: 2, Role: Follower, CurrentTerm: 6, VotedFor: -1, ClusterSize: 3},
		},
		{
			"deposed leader steps down on a higher term",
			State{ID: 0, Role: Leader, CurrentTerm: 4, VotedFor: 0, VotesGranted: 2, ClusterSize: 3},
			Event{Type: EventAppend, Term: 5},
			State{ID: 0, Role: Follower, CurrentTerm: 5, VotedFor: -1, VotesGranted: 0, ClusterSize: 3},
		},
		{
			"follower stays follower on a current-term heartbeat",
			State{ID: 2, Role: Follower, CurrentTerm: 4, VotedFor: 0, ClusterSize: 3},
			Event{Type: EventAppend, Term: 4},
			State{ID: 2, Role: Follower, CurrentTerm: 4, VotedFor: 0, ClusterSize: 3},
		},
		{
			"single-node cluster elects itself instantly",
			State{ID: 0, Role: Follower, CurrentTerm: 0, VotedFor: -1, ClusterSize: 1},
			Event{Type: EventTimeout},
			State{ID: 0, Role: Leader, CurrentTerm: 1, VotedFor: 0, VotesGranted: 1, ClusterSize: 1},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Step(c.in, c.ev); got != c.want {
				t.Errorf("Step(%+v, %+v)\n got  %+v\n want %+v", c.in, c.ev, got, c.want)
			}
		})
	}
}
```

# Lesson: Leader Election {#leader-election}

When a candidate starts an election it sends a **RequestVote RPC** to every
other server in parallel. Figure 2 gives the arguments:

```go
// RequestVote RPC (Figure 2)
// Arguments:
//   term          candidate's term
//   candidateId   candidate requesting vote
//   lastLogIndex  index of candidate's last log entry (§5.4)
//   lastLogTerm   term of candidate's last log entry (§5.4)
// Results:
//   term          currentTerm, for candidate to update itself
//   voteGranted   true means candidate received vote
```

The receiver's side is what you implement in this challenge. Figure 2's
"RequestVote RPC, Receiver implementation" is only two lines, but every clause
matters:

1. Reply false if `term < currentTerm` (§5.1).
2. If `votedFor` is null or `candidateId`, **and** candidate's log is at least
   as up-to-date as receiver's log, grant vote (§5.2, §5.4).

### At most one vote per term

`votedFor` records who this server voted for in the *current* term. Each server
gives out at most one vote per term, first-come-first-served, which is what
makes two leaders in one term impossible: two candidates would each need a
majority, and majorities overlap. Note the "or `candidateId`" clause — if the
same candidate asks twice (a retransmitted RPC), the vote is granted again.
Granting a vote is idempotent, not a second vote.

Whenever `currentTerm` changes (rule "All Servers" from lesson 1), `votedFor`
resets to none: a new term is a fresh ballot.

In the paper `currentTerm`, `votedFor`, and the log are **persistent state** —
a real server must fsync them before answering an RPC, or a reboot could let it
vote twice in one term. Our in-memory structs stand in for that durable state.

### The up-to-dateness check (§5.4.1)

The log check is Raft's election-time safety filter: a candidate cannot win
unless its log is *at least as up-to-date* as a majority of voters, which
guarantees the winner already holds every committed entry (the Leader
Completeness Property). "Up-to-date" is defined precisely:

> If the logs have last entries with different terms, then the log with the
> later term is more up-to-date. If the logs end with the same term, then
> whichever log is longer is more up-to-date.

Compare **last log term first, then last log index**. A short log ending in a
high term beats a long log ending in a low term.

### Indexing convention

The paper numbers log entries from 1. We keep that: Raft index `i` lives at
`Log[i-1]`, the last index is `len(Log)`, and an empty log has last index 0 and
last term 0. This convention holds for the rest of the course.

## Challenge: Handle RequestVote {#request-vote points=30}

Implement `HandleRequestVote` exactly per Figure 2. It mutates the receiver's
state in place and returns the reply.

Order of operations:

1. If `args.Term > s.CurrentTerm`: set `CurrentTerm = args.Term`, become
   `Follower`, reset `VotedFor = -1`. This happens **even if the vote is then
   refused** for log reasons.
2. Reply false if `args.Term < s.CurrentTerm`. The reply's `Term` is always the
   (possibly just-updated) `CurrentTerm`.
3. Grant the vote only if `VotedFor` is `-1` **or** already `args.CandidateID`,
   and the candidate's log is at least as up-to-date as ours: compare
   `args.LastLogTerm` against our last log term, breaking ties with
   `args.LastLogIndex` against our last log index (candidate wins ties).
4. On granting, record `VotedFor = args.CandidateID`.

`HandleRequestVote` never modifies the log.

### Starter

```go
package challenge

// Role is one of the three server states from §5.1 of the Raft paper.
type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

// Entry is one log entry: the term in which it was created plus a command.
type Entry struct {
	Term    int
	Command string
}

// ServerState holds the receiver's Raft state. Raft log index i (1-based)
// lives at Log[i-1]; an empty log has last index 0 and last term 0.
type ServerState struct {
	Role        Role
	CurrentTerm int
	VotedFor    int // candidate ID voted for in CurrentTerm; -1 = none
	Log         []Entry
}

// RequestVoteArgs mirrors the RPC arguments of Figure 2.
type RequestVoteArgs struct {
	Term         int // candidate's term
	CandidateID  int // candidate requesting the vote
	LastLogIndex int // index of candidate's last log entry
	LastLogTerm  int // term of candidate's last log entry
}

// RequestVoteReply mirrors the RPC results of Figure 2.
type RequestVoteReply struct {
	Term        int // currentTerm, for the candidate to update itself
	VoteGranted bool
}

// HandleRequestVote applies Figure 2's receiver implementation.
func HandleRequestVote(s *ServerState, args RequestVoteArgs) RequestVoteReply {
	// TODO: term rules, votedFor rules, and the §5.4.1 up-to-dateness check.
	return RequestVoteReply{}
}
```

### Tests

```go
package challenge

import (
	"reflect"
	"testing"
)

func TestHandleRequestVote(t *testing.T) {
	cases := []struct {
		name      string
		state     ServerState
		args      RequestVoteArgs
		wantReply RequestVoteReply
		wantState ServerState
	}{
		{
			"rejects a stale term",
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}}},
			RequestVoteArgs{Term: 4, CandidateID: 2, LastLogIndex: 5, LastLogTerm: 4},
			RequestVoteReply{Term: 5, VoteGranted: false},
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}}},
		},
		{
			"grants the first vote of the term",
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}}},
			RequestVoteArgs{Term: 5, CandidateID: 2, LastLogIndex: 2, LastLogTerm: 1},
			RequestVoteReply{Term: 5, VoteGranted: true},
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: 2, Log: []Entry{{1, "a"}, {1, "b"}}},
		},
		{
			"at most one vote per term",
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: 3, Log: []Entry{{1, "a"}, {1, "b"}}},
			RequestVoteArgs{Term: 5, CandidateID: 2, LastLogIndex: 2, LastLogTerm: 1},
			RequestVoteReply{Term: 5, VoteGranted: false},
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: 3, Log: []Entry{{1, "a"}, {1, "b"}}},
		},
		{
			"repeat request from the same candidate is re-granted",
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: 2, Log: []Entry{{1, "a"}, {1, "b"}}},
			RequestVoteArgs{Term: 5, CandidateID: 2, LastLogIndex: 2, LastLogTerm: 1},
			RequestVoteReply{Term: 5, VoteGranted: true},
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: 2, Log: []Entry{{1, "a"}, {1, "b"}}},
		},
		{
			"higher term resets votedFor, then grants",
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: 3, Log: []Entry{{1, "a"}, {1, "b"}}},
			RequestVoteArgs{Term: 6, CandidateID: 2, LastLogIndex: 2, LastLogTerm: 1},
			RequestVoteReply{Term: 6, VoteGranted: true},
			ServerState{Role: Follower, CurrentTerm: 6, VotedFor: 2, Log: []Entry{{1, "a"}, {1, "b"}}},
		},
		{
			"leader steps down on a higher-term request",
			ServerState{Role: Leader, CurrentTerm: 5, VotedFor: 1, Log: []Entry{{1, "a"}, {1, "b"}}},
			RequestVoteArgs{Term: 6, CandidateID: 2, LastLogIndex: 2, LastLogTerm: 1},
			RequestVoteReply{Term: 6, VoteGranted: true},
			ServerState{Role: Follower, CurrentTerm: 6, VotedFor: 2, Log: []Entry{{1, "a"}, {1, "b"}}},
		},
		{
			"higher term with a stale log: term adopted, vote refused",
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: 3, Log: []Entry{{1, "a"}, {3, "b"}}},
			RequestVoteArgs{Term: 6, CandidateID: 2, LastLogIndex: 5, LastLogTerm: 2},
			RequestVoteReply{Term: 6, VoteGranted: false},
			ServerState{Role: Follower, CurrentTerm: 6, VotedFor: -1, Log: []Entry{{1, "a"}, {3, "b"}}},
		},
		{
			"lower last term is rejected despite a longer log",
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{1, "a"}, {2, "b"}}},
			RequestVoteArgs{Term: 5, CandidateID: 2, LastLogIndex: 10, LastLogTerm: 1},
			RequestVoteReply{Term: 5, VoteGranted: false},
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{1, "a"}, {2, "b"}}},
		},
		{
			"same last term, shorter log is rejected",
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}},
			RequestVoteArgs{Term: 5, CandidateID: 2, LastLogIndex: 2, LastLogTerm: 1},
			RequestVoteReply{Term: 5, VoteGranted: false},
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}},
		},
		{
			"same last term, same length is granted",
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}},
			RequestVoteArgs{Term: 5, CandidateID: 2, LastLogIndex: 3, LastLogTerm: 1},
			RequestVoteReply{Term: 5, VoteGranted: true},
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: 2, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}},
		},
		{
			"higher last term beats a longer log",
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}},
			RequestVoteArgs{Term: 5, CandidateID: 2, LastLogIndex: 1, LastLogTerm: 2},
			RequestVoteReply{Term: 5, VoteGranted: true},
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: 2, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}},
		},
		{
			"two empty logs: vote granted",
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1},
			RequestVoteArgs{Term: 1, CandidateID: 2, LastLogIndex: 0, LastLogTerm: 0},
			RequestVoteReply{Term: 1, VoteGranted: true},
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: 2},
		},
		{
			"empty candidate log vs non-empty voter log: rejected",
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1, Log: []Entry{{1, "a"}}},
			RequestVoteArgs{Term: 1, CandidateID: 2, LastLogIndex: 0, LastLogTerm: 0},
			RequestVoteReply{Term: 1, VoteGranted: false},
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1, Log: []Entry{{1, "a"}}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := c.state
			got := HandleRequestVote(&state, c.args)
			if got != c.wantReply {
				t.Errorf("reply = %+v, want %+v", got, c.wantReply)
			}
			if !reflect.DeepEqual(state, c.wantState) {
				t.Errorf("state after call\n got  %+v\n want %+v", state, c.wantState)
			}
		})
	}
}
```

# Lesson: Log Replication {#log-replication}

Once elected, a leader services client requests: it appends each command to its
own log, then replicates the entry to the followers with **AppendEntries
RPCs** — the same RPC that, sent with no entries, doubles as the heartbeat.

The key invariant is the **Log Matching Property** (§5.3):

> If two logs contain an entry with the same index and term, then the logs are
> identical in all entries up through the given index.

Raft maintains it with a *consistency check*: every AppendEntries request
carries `prevLogIndex` and `prevLogTerm`, the coordinates of the entry
immediately preceding the new ones. The follower accepts the entries only if
its own log contains an entry at `prevLogIndex` with term `prevLogTerm`. If the
check fails, the leader retries with a smaller `prevLogIndex` until the logs
agree on a prefix — an induction step that repairs any divergence.

### Figure 2, receiver implementation

You will implement these five steps *literally*:

1. Reply false if `term < currentTerm` (§5.1).
2. Reply false if log doesn't contain an entry at `prevLogIndex` whose term
   matches `prevLogTerm` (§5.3). `prevLogIndex = 0` — the empty prefix —
   always matches.
3. If an existing entry conflicts with a new one (same index but different
   terms), delete the existing entry and all that follow it (§5.3).
4. Append any new entries **not already in the log**.
5. If `leaderCommit > commitIndex`, set
   `commitIndex = min(leaderCommit, index of last new entry)`.

Steps 3 and 4 are where most buggy implementations die. Note what they do *not*
say: they do not say "truncate the log to end at the last new entry". RPCs can
be duplicated and can arrive containing entries the follower already has. If
every incoming entry matches what is already in the log, the follower must
change **nothing** — in particular it must not chop off entries after the
matched prefix, because those entries may already be committed. Truncation
happens only at an actual *conflict*: same index, different term.

Step 5's `min` also deserves a close read. "Index of last new entry" means
`prevLogIndex + len(entries)` — the last index this particular RPC vouches for.
A heartbeat that matched a prefix of your log at `prevLogIndex = 2` only proves
entries up to index 2 match the leader's log, so even if `leaderCommit` is 5
you may only advance `commitIndex` to 2 for now.

Finally, the term rules from lesson 1 still apply: a request with a higher term
updates `currentTerm` (resetting `VotedFor`), and any *valid* AppendEntries —
same term or higher — makes the receiver a follower. That is how a candidate
that lost the race, or a deposed leader, gets back in line.

## Challenge: Handle AppendEntries {#append-entries points=40}

Implement `HandleAppendEntries` following Figure 2's five receiver steps plus
the term rules. It mutates the receiver in place and returns the reply.

Precise order:

1. If `args.Term > s.CurrentTerm`: adopt the term and reset `VotedFor = -1`.
2. Reply false (with the current term) if `args.Term < s.CurrentTerm`.
3. The request is now from the legitimate current leader: set
   `Role = Follower`.
4. Consistency check: reply false if `args.PrevLogIndex > len(s.Log)`, or if
   `args.PrevLogIndex > 0` and `s.Log[args.PrevLogIndex-1].Term !=
   args.PrevLogTerm`. A failed check must not modify the log.
5. Walk `args.Entries`: entry `i` belongs at Raft index
   `args.PrevLogIndex + 1 + i`. Skip entries that already match (same index,
   same term). At the first conflict, truncate the log just before it and
   append the remaining entries. Entries past the end of the log are appended.
6. If `args.LeaderCommit > s.CommitIndex`, set
   `s.CommitIndex = min(args.LeaderCommit, args.PrevLogIndex+len(args.Entries))`.
7. Reply true.

### Starter

```go
package challenge

// Role is one of the three server states from §5.1 of the Raft paper.
type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

// Entry is one log entry: the term in which it was created plus a command.
type Entry struct {
	Term    int
	Command string
}

// ServerState holds the receiver's Raft state. Raft log index i (1-based)
// lives at Log[i-1]; an empty log has last index 0 and last term 0.
type ServerState struct {
	Role        Role
	CurrentTerm int
	VotedFor    int // candidate ID voted for in CurrentTerm; -1 = none
	Log         []Entry
	CommitIndex int // highest log index known committed
}

// AppendEntriesArgs mirrors the RPC arguments of Figure 2.
type AppendEntriesArgs struct {
	Term         int     // leader's term
	LeaderID     int     // so followers can redirect clients
	PrevLogIndex int     // index of the entry immediately preceding Entries
	PrevLogTerm  int     // term of the prevLogIndex entry
	Entries      []Entry // entries to store (empty for heartbeat)
	LeaderCommit int     // leader's commitIndex
}

// AppendEntriesReply mirrors the RPC results of Figure 2.
type AppendEntriesReply struct {
	Term    int // currentTerm, for the leader to update itself
	Success bool
}

// HandleAppendEntries applies Figure 2's receiver implementation.
func HandleAppendEntries(s *ServerState, args AppendEntriesArgs) AppendEntriesReply {
	// TODO: the five receiver steps, plus the term rules.
	return AppendEntriesReply{}
}
```

### Tests

```go
package challenge

import (
	"reflect"
	"testing"
)

func TestHandleAppendEntries(t *testing.T) {
	cases := []struct {
		name      string
		state     ServerState
		args      AppendEntriesArgs
		wantReply AppendEntriesReply
		wantState ServerState
	}{
		{
			"rejects a stale term",
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{1, "a"}}},
			AppendEntriesArgs{Term: 4, LeaderID: 9, PrevLogIndex: 1, PrevLogTerm: 1, Entries: []Entry{{4, "z"}}, LeaderCommit: 1},
			AppendEntriesReply{Term: 5, Success: false},
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{1, "a"}}},
		},
		{
			"adopts a higher term, resets votedFor, appends",
			ServerState{Role: Candidate, CurrentTerm: 3, VotedFor: 7},
			AppendEntriesArgs{Term: 5, LeaderID: 1, PrevLogIndex: 0, PrevLogTerm: 0, Entries: []Entry{{5, "a"}}},
			AppendEntriesReply{Term: 5, Success: true},
			ServerState{Role: Follower, CurrentTerm: 5, VotedFor: -1, Log: []Entry{{5, "a"}}},
		},
		{
			"candidate yields to a same-term leader",
			ServerState{Role: Candidate, CurrentTerm: 4, VotedFor: 7},
			AppendEntriesArgs{Term: 4, LeaderID: 1, PrevLogIndex: 0, PrevLogTerm: 0},
			AppendEntriesReply{Term: 4, Success: true},
			ServerState{Role: Follower, CurrentTerm: 4, VotedFor: 7},
		},
		{
			"prevLogIndex beyond the end of the log fails",
			ServerState{Role: Follower, CurrentTerm: 2, VotedFor: -1, Log: []Entry{{1, "a"}}},
			AppendEntriesArgs{Term: 2, LeaderID: 1, PrevLogIndex: 3, PrevLogTerm: 1, Entries: []Entry{{2, "x"}}},
			AppendEntriesReply{Term: 2, Success: false},
			ServerState{Role: Follower, CurrentTerm: 2, VotedFor: -1, Log: []Entry{{1, "a"}}},
		},
		{
			"prevLogTerm mismatch fails without touching the log",
			ServerState{Role: Follower, CurrentTerm: 3, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}}},
			AppendEntriesArgs{Term: 3, LeaderID: 1, PrevLogIndex: 2, PrevLogTerm: 2, Entries: []Entry{{3, "x"}}},
			AppendEntriesReply{Term: 3, Success: false},
			ServerState{Role: Follower, CurrentTerm: 3, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}}},
		},
		{
			"appends onto an empty log",
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1},
			AppendEntriesArgs{Term: 1, LeaderID: 1, PrevLogIndex: 0, PrevLogTerm: 0, Entries: []Entry{{1, "a"}, {1, "b"}}},
			AppendEntriesReply{Term: 1, Success: true},
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}}},
		},
		{
			"conflicting entry truncates the tail",
			ServerState{Role: Follower, CurrentTerm: 3, VotedFor: -1, Log: []Entry{{1, "a"}, {2, "b"}, {2, "c"}}},
			AppendEntriesArgs{Term: 3, LeaderID: 1, PrevLogIndex: 1, PrevLogTerm: 1, Entries: []Entry{{3, "x"}, {3, "y"}}},
			AppendEntriesReply{Term: 3, Success: true},
			ServerState{Role: Follower, CurrentTerm: 3, VotedFor: -1, Log: []Entry{{1, "a"}, {3, "x"}, {3, "y"}}},
		},
		{
			"duplicate append is idempotent and never truncates",
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}},
			AppendEntriesArgs{Term: 1, LeaderID: 1, PrevLogIndex: 0, PrevLogTerm: 0, Entries: []Entry{{1, "a"}, {1, "b"}}},
			AppendEntriesReply{Term: 1, Success: true},
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}},
		},
		{
			"heartbeat with a stale prefix: no truncation, commit capped at prevLogIndex",
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}},
			AppendEntriesArgs{Term: 1, LeaderID: 1, PrevLogIndex: 2, PrevLogTerm: 1, LeaderCommit: 3},
			AppendEntriesReply{Term: 1, Success: true},
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}, CommitIndex: 2},
		},
		{
			"commitIndex capped by the index of the last new entry",
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1},
			AppendEntriesArgs{Term: 1, LeaderID: 1, PrevLogIndex: 0, PrevLogTerm: 0, Entries: []Entry{{1, "a"}, {1, "b"}}, LeaderCommit: 10},
			AppendEntriesReply{Term: 1, Success: true},
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}}, CommitIndex: 2},
		},
		{
			"commitIndex follows leaderCommit when it is smaller",
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1},
			AppendEntriesArgs{Term: 1, LeaderID: 1, PrevLogIndex: 0, PrevLogTerm: 0, Entries: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}, LeaderCommit: 2},
			AppendEntriesReply{Term: 1, Success: true},
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}, CommitIndex: 2},
		},
		{
			"partial overlap appends only the genuinely new tail",
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}}},
			AppendEntriesArgs{Term: 1, LeaderID: 1, PrevLogIndex: 1, PrevLogTerm: 1, Entries: []Entry{{1, "b"}, {1, "c"}}},
			AppendEntriesReply{Term: 1, Success: true},
			ServerState{Role: Follower, CurrentTerm: 1, VotedFor: -1, Log: []Entry{{1, "a"}, {1, "b"}, {1, "c"}}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := c.state
			state.Log = append([]Entry(nil), c.state.Log...)
			got := HandleAppendEntries(&state, c.args)
			if got != c.wantReply {
				t.Errorf("reply = %+v, want %+v", got, c.wantReply)
			}
			want := c.wantState
			if len(want.Log) == 0 {
				want.Log = nil
			}
			if len(state.Log) == 0 {
				state.Log = nil
			}
			if !reflect.DeepEqual(state, want) {
				t.Errorf("state after call\n got  %+v\n want %+v", state, want)
			}
		})
	}
}
```

# Lesson: Commitment and Safety {#commitment-and-safety}

An entry is **committed** once it is safe to apply to the state machine — Raft
then guarantees it will survive any future leader change. The leader tracks,
for every follower, `matchIndex`: the highest log index known to be replicated
on that server. Figure 2's "Rules for Servers, Leaders" gives the commit rule:

> If there exists an N such that `N > commitIndex`, a majority of
> `matchIndex[i] ≥ N`, and `log[N].term == currentTerm`: set `commitIndex = N`
> (§5.3, §5.4).

Two of those clauses are obvious — pick something new (`N > commitIndex`) that
a majority stores. The third clause, `log[N].term == currentTerm`, is the
subtle heart of Raft's safety argument.

### Why majority replication is not enough (§5.4.2, Figure 8)

Figure 8 of the paper walks through the trap on a five-server cluster,
S1–S5. In (a), S1 — leader in term 2 — appends an entry at index 2 but
replicates it to only one other server (S2) before crashing: a *minority*.
In (b), S5 wins the next election (term 3, with votes from S3, S4, and
itself) and writes a *different* entry at index 2. S5 crashes too; in (c),
S1 restarts, wins election again as leader of term 4, and resumes normal
replication. As a side effect of replicating its own term-4 entries, S1's
old term-2 entry at index 2 now happens to sit on a majority of the cluster
(S1, S2, and S3) — but nobody ever committed it. May a leader now count
those replicas and consider index 2 committed? **No.** In (d), S5 — whose
log still ends in the higher term 3, so it looks "up-to-date" by §5.4.1 even
though it is missing S1's index-2 entry entirely — wins a later election
from S2, S3, and S4, and overwrites index 2 with its own term-3 entry. If
counting replicas had been enough to call index 2 committed back in (c),
this would be a lost write: the one unforgivable sin of a consensus
algorithm.

The fix: a leader only ever commits by counting replicas for entries **from its
own current term**. Old-term entries are never committed directly. They get
committed *indirectly*: the moment a current-term entry at a later index
commits, the Log Matching Property guarantees every earlier entry in that log —
including the old-term stragglers — is committed with it.

So in the healthy case the sequence is: leader appends in its own term,
replicates, counts a majority, advances `commitIndex` past everything. In the
Figure 8 case the old entry simply waits for the new leader to commit its first
own-term entry (real implementations append a no-op at the start of a term for
exactly this reason).

### Counting the leader itself

The leader is a member of the majority too. In this challenge we sidestep the
bookkeeping by passing a `matchIndex` slice with **one element per server in
the cluster, leader included** (the leader's own element is simply
`len(log)`). A majority means strictly more than half of that slice.

## Challenge: Advance the Commit Index {#advance-commit points=30}

Implement `AdvanceCommit`. Given the cluster's `matchIndex` (one element per
server, leader included), the leader's log, `currentTerm`, and the current
`commitIndex`, return the new commit index: the **largest** `N` with

- `commitIndex < N <= len(log)`,
- `matchIndex[i] >= N` for a strict majority of servers, and
- `log[N-1].Term == currentTerm` (remember: Raft index `N` lives at
  `log[N-1]`).

If no such `N` exists, return `commitIndex` unchanged. The function is pure —
no mutation, no side effects.

### Starter

```go
package challenge

// Entry is one log entry: the term in which it was created plus a command.
type Entry struct {
	Term    int
	Command string
}

// AdvanceCommit returns the leader's new commitIndex per Figure 2
// ("Rules for Servers, Leaders") and the §5.4.2 current-term restriction.
//
// matchIndex has one element per server in the cluster, leader included
// (the leader's own element is len(log)). Raft indices are 1-based:
// index N is stored at log[N-1].
func AdvanceCommit(matchIndex []int, log []Entry, currentTerm, commitIndex int) int {
	// TODO: find the largest qualifying N, or return commitIndex.
	return 0
}
```

### Tests

```go
package challenge

import "testing"

func TestAdvanceCommit(t *testing.T) {
	cases := []struct {
		name        string
		matchIndex  []int
		log         []Entry
		currentTerm int
		commitIndex int
		want        int
	}{
		{
			"commits a majority-replicated current-term entry",
			[]int{2, 2, 0},
			[]Entry{{1, "a"}, {1, "b"}},
			1, 0,
			2,
		},
		{
			"no quorum beyond the current commit",
			[]int{2, 0, 0},
			[]Entry{{1, "a"}, {1, "b"}},
			1, 1,
			1,
		},
		{
			"figure 8: an old-term entry never commits by counting",
			[]int{2, 2, 2, 1, 1},
			[]Entry{{1, "a"}, {2, "b"}},
			4, 1,
			1,
		},
		{
			"figure 8 resolved: a current-term commit carries old entries with it",
			[]int{3, 3, 3, 1, 1},
			[]Entry{{1, "a"}, {2, "b"}, {4, "noop"}},
			4, 1,
			3,
		},
		{
			"largest qualifying index wins, not just the first",
			[]int{3, 3, 3},
			[]Entry{{1, "a"}, {1, "b"}, {1, "c"}},
			1, 0,
			3,
		},
		{
			"empty log commits nothing",
			[]int{0, 0, 0},
			nil,
			1, 0,
			0,
		},
		{
			"already fully committed",
			[]int{2, 2, 2},
			[]Entry{{1, "a"}, {1, "b"}},
			1, 2,
			2,
		},
		{
			"five servers with staggered matchIndex",
			[]int{5, 4, 2, 1, 1},
			[]Entry{{1, "a"}, {1, "b"}, {1, "c"}, {1, "d"}, {1, "e"}},
			1, 0,
			2,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AdvanceCommit(c.matchIndex, c.log, c.currentTerm, c.commitIndex)
			if got != c.want {
				t.Errorf("AdvanceCommit(%v, %v, term=%d, commit=%d) = %d, want %d",
					c.matchIndex, c.log, c.currentTerm, c.commitIndex, got, c.want)
			}
		})
	}
}
```

# Lesson: Election Timing in a Deterministic World {#election-timing}

Raft's liveness hinges on timing (§5.6, "Timing and availability"). Followers
expect a heartbeat from the leader well before their election timeout expires;
the paper's rule of thumb is

```go
// broadcastTime << electionTimeout << MTBF
```

If every server used the *same* election timeout, they would all time out
together, all become candidates together, split the vote, and repeat forever.
Raft's fix is charmingly low-tech: **randomized election timeouts** (e.g.
150–300 ms). Whoever draws the shortest timeout usually wins the election
before anyone else even wakes up. The paper's own experiments (§9.3,
"Performance") back this recommendation: shrinking the timeout range brings
faster failover, but push it too low (well under the broadcast time) and
leaders start missing their own heartbeat deadline before followers'
timeouts fire, causing unnecessary elections and *lower* availability — which
is exactly the `broadcastTime << electionTimeout` half of the inequality
above. 150–300 ms is the paper's own conservative recommendation, chosen to
avoid that failure mode while still detecting a crashed leader quickly.

### Time as data

Randomness and wall clocks are poison for tests. Production-grade Raft
implementations (etcd's raft library is the famous example) therefore model
time as an integer: the node has no timer at all, just a `Tick()` method the
host calls at a fixed cadence, plus counters:

- `ElectionElapsed` — ticks since the last heartbeat/vote-grant; when it
  reaches `ElectionTimeout`, the election fires.
- `HeartbeatElapsed` — ticks since the leader last broadcast; when it reaches
  `HeartbeatTimeout`, the leader sends heartbeats.

The *randomized* timeout becomes plain data: something outside the node picks
`ElectionTimeout` (randomly in production, explicitly in tests) and stores it
in the struct. A node with `ElectionTimeout = 4` deterministically loses
patience before one with `ElectionTimeout = 6` — which is exactly how we will
stage elections on purpose in the final challenge.

### Nodes emit, they do not send

The second determinism trick: a node never talks to a network. Stepping a node
**returns the messages it wants sent** and lets the caller deliver them. The
node stays a pure state machine; the messy parts (sockets, retries, delays,
partitions) live outside and can be simulated. In this challenge `Tick` returns
a `[]Message`; peers are listed in `Peers`, and to keep everything reproducible
you must emit messages **in `Peers` order**.

What can a tick emit?

- A follower or candidate whose `ElectionElapsed` reaches `ElectionTimeout`
  starts an election exactly as in lesson 1 — and emits a `MsgRequestVote` to
  every peer, carrying its `LastLogIndex`/`LastLogTerm` for the §5.4.1 check.
- A leader whose `HeartbeatElapsed` reaches `HeartbeatTimeout` emits a
  `MsgHeartbeat` (an empty AppendEntries) to every peer, carrying its
  `LeaderCommit` so followers can advance their commit index.
- Everything else: no messages, just a counter bumped.

Leaders never run an election timer — receiving valid heartbeats is the
follower's job, sending them is the leader's.

## Challenge: Drive a Node with Ticks {#tick points=35}

Implement `Tick(n *Node) []Message`, one clock tick for one node.

Rules:

1. **Leader**: increment `HeartbeatElapsed`. If it is still below
   `HeartbeatTimeout`, return no messages. Otherwise reset it to 0 and return
   one `MsgHeartbeat` per peer, in `Peers` order, with `Term = CurrentTerm` and
   `LeaderCommit = CommitIndex`. Leaders never touch `ElectionElapsed`.
2. **Follower or candidate**: increment `ElectionElapsed`. If it is still below
   `ElectionTimeout`, return no messages. Otherwise the election fires: reset
   `ElectionElapsed` to 0, become `Candidate`, increment `CurrentTerm`, set
   `VotedFor = n.ID` and `VotesGranted = 1`. If that is already a majority of
   the cluster (`len(Peers)+1` servers), become `Leader` immediately and return
   no messages. Otherwise return one `MsgRequestVote` per peer, in `Peers`
   order, with `Term = CurrentTerm` and the node's last log index and term
   (1-based; empty log → 0 and 0).

Set `From` and `To` on every message. Fields that do not apply to a message
type stay zero.

### Starter

```go
package challenge

// Role is one of the three server states from §5.1 of the Raft paper.
type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

// Entry is one log entry: the term in which it was created plus a command.
type Entry struct {
	Term    int
	Command string
}

// MessageType tags the messages a ticking node can emit.
type MessageType int

const (
	MsgRequestVote MessageType = iota // candidate soliciting a vote
	MsgHeartbeat                      // leader's empty AppendEntries
)

// Message is an outbound message. Unused fields stay zero.
type Message struct {
	From, To     int
	Type         MessageType
	Term         int
	LastLogIndex int // MsgRequestVote: candidate's last log index
	LastLogTerm  int // MsgRequestVote: candidate's last log term
	LeaderCommit int // MsgHeartbeat: leader's commitIndex
}

// Node is one Raft server with time modeled as tick counters.
// Raft log index i (1-based) lives at Log[i-1].
type Node struct {
	ID    int
	Peers []int // the other servers' IDs; cluster size is len(Peers)+1

	Role         Role
	CurrentTerm  int
	VotedFor     int // -1 = none
	VotesGranted int
	Log          []Entry
	CommitIndex  int

	ElectionElapsed  int // ticks since last heartbeat / vote grant
	ElectionTimeout  int // fires when ElectionElapsed reaches it
	HeartbeatElapsed int // leader only: ticks since last broadcast
	HeartbeatTimeout int // leader broadcasts when reached
}

// Tick advances n's timers by one tick and returns the messages a real
// node would send, in Peers order.
func Tick(n *Node) []Message {
	// TODO: election timeout -> start an election; leader cadence -> heartbeats.
	return nil
}
```

### Tests

```go
package challenge

import (
	"reflect"
	"testing"
)

func TestFollowerQuietTick(t *testing.T) {
	n := &Node{ID: 1, Peers: []int{2, 3}, Role: Follower, CurrentTerm: 1, VotedFor: -1, ElectionTimeout: 3}
	if msgs := Tick(n); len(msgs) != 0 {
		t.Fatalf("quiet tick emitted %+v", msgs)
	}
	if n.ElectionElapsed != 1 {
		t.Fatalf("ElectionElapsed = %d, want 1", n.ElectionElapsed)
	}
	if n.Role != Follower || n.CurrentTerm != 1 {
		t.Fatalf("state changed on a quiet tick: %+v", n)
	}
}

func TestElectionFiresAtTimeout(t *testing.T) {
	n := &Node{
		ID: 1, Peers: []int{2, 3}, Role: Follower, CurrentTerm: 1, VotedFor: -1,
		Log: []Entry{{Term: 1, Command: "a"}}, ElectionTimeout: 3,
	}
	var msgs []Message
	for i := 0; i < 3; i++ {
		msgs = Tick(n)
		if i < 2 && len(msgs) != 0 {
			t.Fatalf("tick %d emitted %+v before the timeout", i+1, msgs)
		}
	}
	if n.Role != Candidate || n.CurrentTerm != 2 || n.VotedFor != 1 || n.VotesGranted != 1 {
		t.Fatalf("after timeout: %+v", n)
	}
	if n.ElectionElapsed != 0 {
		t.Fatalf("ElectionElapsed must reset on firing, got %d", n.ElectionElapsed)
	}
	want := []Message{
		{From: 1, To: 2, Type: MsgRequestVote, Term: 2, LastLogIndex: 1, LastLogTerm: 1},
		{From: 1, To: 3, Type: MsgRequestVote, Term: 2, LastLogIndex: 1, LastLogTerm: 1},
	}
	if !reflect.DeepEqual(msgs, want) {
		t.Fatalf("msgs = %+v, want %+v", msgs, want)
	}
	if extra := Tick(n); len(extra) != 0 {
		t.Fatalf("tick right after firing emitted %+v", extra)
	}
}

func TestCandidateRetriesElection(t *testing.T) {
	n := &Node{
		ID: 0, Peers: []int{1, 2}, Role: Candidate, CurrentTerm: 3, VotedFor: 0,
		VotesGranted: 1, ElectionTimeout: 5, ElectionElapsed: 4,
	}
	msgs := Tick(n)
	if n.Role != Candidate || n.CurrentTerm != 4 || n.VotedFor != 0 || n.VotesGranted != 1 {
		t.Fatalf("after re-election: %+v", n)
	}
	if len(msgs) != 2 || msgs[0].Term != 4 || msgs[1].Term != 4 {
		t.Fatalf("msgs = %+v, want two term-4 vote requests", msgs)
	}
}

func TestLeaderHeartbeatCadence(t *testing.T) {
	n := &Node{
		ID: 0, Peers: []int{1, 2}, Role: Leader, CurrentTerm: 2, VotedFor: 0,
		Log: []Entry{{1, "a"}, {2, "b"}, {2, "c"}}, CommitIndex: 3,
		ElectionTimeout: 4, HeartbeatTimeout: 2,
	}
	if msgs := Tick(n); len(msgs) != 0 {
		t.Fatalf("heartbeat fired one tick early: %+v", msgs)
	}
	msgs := Tick(n)
	want := []Message{
		{From: 0, To: 1, Type: MsgHeartbeat, Term: 2, LeaderCommit: 3},
		{From: 0, To: 2, Type: MsgHeartbeat, Term: 2, LeaderCommit: 3},
	}
	if !reflect.DeepEqual(msgs, want) {
		t.Fatalf("msgs = %+v, want %+v", msgs, want)
	}
	if n.HeartbeatElapsed != 0 {
		t.Fatalf("HeartbeatElapsed must reset after broadcasting, got %d", n.HeartbeatElapsed)
	}
	if n.Role != Leader || n.CurrentTerm != 2 {
		t.Fatalf("heartbeat changed leader state: %+v", n)
	}
}

func TestLeaderIgnoresElectionTimer(t *testing.T) {
	n := &Node{
		ID: 0, Peers: []int{1, 2}, Role: Leader, CurrentTerm: 2, VotedFor: 0,
		ElectionTimeout: 2, HeartbeatTimeout: 10,
	}
	for i := 0; i < 5; i++ {
		if msgs := Tick(n); len(msgs) != 0 {
			t.Fatalf("tick %d emitted %+v", i+1, msgs)
		}
	}
	if n.Role != Leader || n.CurrentTerm != 2 {
		t.Fatalf("leader started an election against itself: %+v", n)
	}
}

func TestSingleNodeClusterElectsItself(t *testing.T) {
	n := &Node{ID: 0, Role: Follower, CurrentTerm: 0, VotedFor: -1, ElectionTimeout: 1}
	msgs := Tick(n)
	if len(msgs) != 0 {
		t.Fatalf("single-node cluster has no one to message, got %+v", msgs)
	}
	if n.Role != Leader || n.CurrentTerm != 1 || n.VotedFor != 0 || n.VotesGranted != 1 {
		t.Fatalf("single node should be leader of term 1: %+v", n)
	}
}

func TestMessagesFollowPeerOrder(t *testing.T) {
	n := &Node{ID: 5, Peers: []int{7, 2}, Role: Follower, CurrentTerm: 0, VotedFor: -1, ElectionTimeout: 1}
	msgs := Tick(n)
	if len(msgs) != 2 || msgs[0].To != 7 || msgs[1].To != 2 {
		t.Fatalf("msgs = %+v, want destinations [7 2] in Peers order", msgs)
	}
}
```

# Final Challenge: A Deterministic Cluster {#cluster-simulation points=100}

Time to assemble the machine. You get a three-node in-memory cluster where
**time is ticks and the network is a queue**, and you implement the entire Raft
node logic — election, log replication, and commitment — as two pure functions:

- `Tick(n *Node) []Message` — one clock tick (lesson 5).
- `Step(n *Node, m Message) []Message` — apply one incoming message, return the
  replies (lessons 1–4).

The `Cluster` harness is already written for you — read it, leave it alone. It
ticks nodes in ID order, queues whatever they emit, and `Deliver`/`DeliverAll`
pump the queue FIFO, dropping messages that cross a partition (`Isolate`/
`Heal`). `Propose` appends a client command to a node's log stamped with that
node's current term. The tests stage everything deterministically: election
timeouts are `[4, 6, 8]` ticks for nodes 0, 1, 2, heartbeats every 2 ticks — so
node 0 always wins the first election, and node 1 takes over when node 0 is
isolated.

Implement to this spec (Figure 2, restated for our tick model):

**`Tick`** — as in lesson 5, with one upgrade: a leader's broadcast is a full
`MsgAppendRequest` per peer, in `Peers` order, carrying
`Entries = Log[NextIndex[peer]-1:]` (copy the slice), `PrevLogIndex =
NextIndex[peer]-1` with the matching `PrevLogTerm`, and
`LeaderCommit = CommitIndex`. When there is nothing new for a peer this is
naturally an empty heartbeat.

**`Step`** — for every message: if `m.Term > n.CurrentTerm`, adopt the term,
become `Follower`, reset `VotedFor` to -1 and `VotesGranted` to 0, then keep
processing. Then by type:

- `MsgVoteRequest`: the lesson-2 receiver rules. Always reply with a
  `MsgVoteReply` carrying your `CurrentTerm`; on granting, also reset
  `ElectionElapsed` to 0.
- `MsgVoteReply`: only candidates count them, only if `Granted` and
  `m.Term == CurrentTerm`. On reaching a majority of `len(Peers)+1`, become
  leader: for every peer set `NextIndex[peer] = len(Log)+1` and
  `MatchIndex[peer] = 0`, reset `HeartbeatElapsed` to 0, and **immediately
  return a first round of heartbeats** (same form as a leader tick) to assert
  authority.
- `MsgAppendRequest`: the lesson-3 receiver rules. Any valid request (term not
  stale) makes you a `Follower` and resets `ElectionElapsed`. Reply with a
  `MsgAppendReply` carrying your `CurrentTerm`, `Success`, and — when
  successful — `MatchIndex = m.PrevLogIndex + len(m.Entries)`.
- `MsgAppendReply`: leaders only, and only for `m.Term == CurrentTerm`. On
  `Success`: raise `MatchIndex[m.From]` and `NextIndex[m.From]` (to
  `m.MatchIndex` and `m.MatchIndex+1`; never lower them), then advance
  `CommitIndex` with the lesson-4 rule — largest `N > CommitIndex` with
  `Log[N-1].Term == CurrentTerm` stored on a majority, counting yourself. On
  failure: decrement `NextIndex[m.From]` (not below 1); the retry rides the
  next heartbeat tick.

Every reply's `From`/`To` must be set (`From = n.ID`, `To = m.From`). If a
message needs no reply, return nothing for it.

The tests walk the cluster through the full Raft story: nothing happens before
the first timeout; node 0 is elected in term 1; a proposed command replicates
and commits on a majority, with followers learning the commit index one
heartbeat later; node 0 is partitioned away, node 1 wins term 2 with node 2's
vote while the stale leader keeps ruling its network of one; and after the
partition heals, the old leader sees term 2, steps down, and converges on the
new leader's log.

### Starter

```go
package challenge

// ---------- Roles, entries, messages ----------

// Role is one of the three server states from §5.1 of the Raft paper.
type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

// Entry is one log entry: the term in which it was created plus a command.
type Entry struct {
	Term    int
	Command string
}

// MessageType tags every RPC flavor in the system.
type MessageType int

const (
	MsgVoteRequest   MessageType = iota // RequestVote RPC
	MsgVoteReply                        // RequestVote response
	MsgAppendRequest                    // AppendEntries RPC (incl. heartbeats)
	MsgAppendReply                      // AppendEntries response
)

// Message is every RPC flattened into one struct; unused fields stay zero.
type Message struct {
	From, To int
	Type     MessageType
	Term     int

	// MsgVoteRequest
	LastLogIndex int
	LastLogTerm  int

	// MsgVoteReply
	Granted bool

	// MsgAppendRequest
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []Entry
	LeaderCommit int

	// MsgAppendReply
	Success    bool
	MatchIndex int // on Success: highest index known replicated on the sender
}

// Node is one Raft server. Raft log index i (1-based) lives at Log[i-1].
type Node struct {
	ID    int
	Peers []int // the other servers' IDs, ascending

	Role         Role
	CurrentTerm  int
	VotedFor     int // -1 = none
	VotesGranted int
	Log          []Entry
	CommitIndex  int

	NextIndex  map[int]int // leader: next index to send each peer
	MatchIndex map[int]int // leader: highest index replicated on each peer

	ElectionElapsed  int
	ElectionTimeout  int
	HeartbeatElapsed int
	HeartbeatTimeout int
}

// ---------- Cluster plumbing (already done — do not change) ----------

// Cluster is a synchronous in-memory network: ticks make nodes emit
// messages into Queue, and Deliver pumps them back into nodes.
type Cluster struct {
	Nodes    []*Node
	Queue    []Message
	isolated map[int]bool
}

// NewCluster builds len(electionTimeouts) nodes with IDs 0..n-1.
func NewCluster(electionTimeouts []int, heartbeatTimeout int) *Cluster {
	c := &Cluster{isolated: map[int]bool{}}
	n := len(electionTimeouts)
	for id := 0; id < n; id++ {
		var peers []int
		for p := 0; p < n; p++ {
			if p != id {
				peers = append(peers, p)
			}
		}
		c.Nodes = append(c.Nodes, &Node{
			ID:               id,
			Peers:            peers,
			VotedFor:         -1,
			ElectionTimeout:  electionTimeouts[id],
			HeartbeatTimeout: heartbeatTimeout,
		})
	}
	return c
}

// TickAll advances time by one tick on every node, in ID order.
func (c *Cluster) TickAll() {
	for _, n := range c.Nodes {
		c.Queue = append(c.Queue, Tick(n)...)
	}
}

// Propose appends a client command to a node's log (call it on the leader).
func (c *Cluster) Propose(id int, command string) {
	n := c.Nodes[id]
	n.Log = append(n.Log, Entry{Term: n.CurrentTerm, Command: command})
}

// Isolate cuts every link between one node and the rest of the cluster.
func (c *Cluster) Isolate(id int) { c.isolated[id] = true }

// Heal restores all links.
func (c *Cluster) Heal() { c.isolated = map[int]bool{} }

// Deliver hands the next in-flight message to its destination, silently
// dropping messages that cross the partition. Reports whether any message
// was actually delivered.
func (c *Cluster) Deliver() bool {
	for len(c.Queue) > 0 {
		m := c.Queue[0]
		c.Queue = c.Queue[1:]
		if c.isolated[m.From] != c.isolated[m.To] {
			continue // dropped at the partition boundary
		}
		c.Queue = append(c.Queue, Step(c.Nodes[m.To], m)...)
		return true
	}
	return false
}

// DeliverAll pumps messages until the network is quiet.
func (c *Cluster) DeliverAll() {
	for i := 0; i < 10000; i++ {
		if !c.Deliver() {
			return
		}
	}
}

// ---------- Your part: the Raft logic ----------

// Tick advances n's timers by one tick and returns the messages to send,
// in Peers order. Election timeout fires an election; a leader reaching
// its heartbeat timeout broadcasts AppendEntries built from NextIndex.
func Tick(n *Node) []Message {
	// TODO
	return nil
}

// Step applies one incoming message to n and returns any replies (plus,
// on winning an election, the new leader's first round of heartbeats).
func Step(n *Node, m Message) []Message {
	// TODO: the four message types, per Figure 2.
	return nil
}
```

### Tests

```go
package challenge

import (
	"reflect"
	"testing"
)

func commands(log []Entry) []string {
	var out []string
	for _, e := range log {
		out = append(out, e.Command)
	}
	return out
}

// electedCluster returns a 3-node cluster where node 0 (shortest timeout)
// has just won term 1.
func electedCluster(t *testing.T) *Cluster {
	t.Helper()
	c := NewCluster([]int{4, 6, 8}, 2)
	for i := 0; i < 4; i++ {
		c.TickAll()
	}
	c.DeliverAll()
	if c.Nodes[0].Role != Leader {
		t.Fatalf("node 0 should be leader after its timeout fires; state: %+v", c.Nodes[0])
	}
	return c
}

// pump runs two full heartbeat rounds: one to replicate, one to spread
// the advanced commit index.
func pump(c *Cluster) {
	for round := 0; round < 2; round++ {
		c.TickAll()
		c.TickAll()
		c.DeliverAll()
	}
}

func TestInitialElection(t *testing.T) {
	c := NewCluster([]int{4, 6, 8}, 2)
	for i := 0; i < 3; i++ {
		c.TickAll()
	}
	c.DeliverAll()
	for _, n := range c.Nodes {
		if n.Role != Follower {
			t.Fatalf("node %d left follower state before any timeout fired: %+v", n.ID, n)
		}
	}

	c.TickAll() // node 0 reaches its election timeout of 4
	c.DeliverAll()

	if c.Nodes[0].Role != Leader {
		t.Fatalf("node 0 should be leader; state: %+v", c.Nodes[0])
	}
	if c.Nodes[0].CurrentTerm != 1 {
		t.Fatalf("leader term = %d, want 1", c.Nodes[0].CurrentTerm)
	}
	for _, id := range []int{1, 2} {
		n := c.Nodes[id]
		if n.Role != Follower {
			t.Errorf("node %d role after election = %v, want Follower", id, n.Role)
		}
		if n.CurrentTerm != 1 {
			t.Errorf("node %d term = %d, want 1", id, n.CurrentTerm)
		}
		if n.VotedFor != 0 {
			t.Errorf("node %d votedFor = %d, want 0", id, n.VotedFor)
		}
	}
}

func TestReplicationAndCommit(t *testing.T) {
	c := electedCluster(t)
	c.Propose(0, "x")

	c.TickAll()
	c.TickAll() // heartbeat timeout = 2: the leader replicates
	c.DeliverAll()

	for _, n := range c.Nodes {
		if got := commands(n.Log); !reflect.DeepEqual(got, []string{"x"}) {
			t.Fatalf("node %d log = %v, want [x]", n.ID, got)
		}
	}
	if got := c.Nodes[0].CommitIndex; got != 1 {
		t.Fatalf("leader commitIndex = %d, want 1 (majority replication in current term)", got)
	}
	if c.Nodes[1].CommitIndex != 0 || c.Nodes[2].CommitIndex != 0 {
		t.Fatalf("followers learn the commit only on the next AppendEntries; got %d and %d",
			c.Nodes[1].CommitIndex, c.Nodes[2].CommitIndex)
	}

	c.TickAll()
	c.TickAll()
	c.DeliverAll() // the next heartbeat carries leaderCommit = 1
	for _, n := range c.Nodes {
		if n.CommitIndex != 1 {
			t.Errorf("node %d commitIndex = %d, want 1", n.ID, n.CommitIndex)
		}
	}
}

func TestPartitionAndHeal(t *testing.T) {
	c := electedCluster(t)
	c.Propose(0, "x")
	pump(c) // "x" replicated and committed everywhere

	c.Isolate(0)
	for i := 0; i < 6; i++ {
		c.TickAll() // node 1 reaches its timeout of 6; node 0 heartbeats into the void
	}
	c.DeliverAll()

	if got := c.Nodes[1].Role; got != Leader {
		t.Fatalf("node 1 role = %v, want Leader of term 2; state: %+v", got, c.Nodes[1])
	}
	if got := c.Nodes[1].CurrentTerm; got != 2 {
		t.Fatalf("node 1 term = %d, want 2", got)
	}
	if got := c.Nodes[2].VotedFor; got != 1 {
		t.Fatalf("node 2 votedFor = %d, want 1", got)
	}
	if got := c.Nodes[0].Role; got != Leader {
		t.Fatalf("isolated node 0 cannot have learned about term 2 yet; role = %v, want (stale) Leader", got)
	}
	if got := c.Nodes[0].CurrentTerm; got != 1 {
		t.Fatalf("isolated node 0 term = %d, want 1", got)
	}

	c.Heal()
	c.Propose(1, "y")
	c.TickAll()
	c.TickAll() // node 1's heartbeat reaches everyone, including the old leader
	c.DeliverAll()

	if got := c.Nodes[0].Role; got != Follower {
		t.Fatalf("old leader role = %v, want Follower after seeing term 2", got)
	}
	if got := c.Nodes[0].CurrentTerm; got != 2 {
		t.Fatalf("old leader term = %d, want 2", got)
	}
	want := []string{"x", "y"}
	for _, n := range c.Nodes {
		if got := commands(n.Log); !reflect.DeepEqual(got, want) {
			t.Errorf("node %d log = %v, want %v", n.ID, got, want)
		}
	}

	c.TickAll()
	c.TickAll()
	c.DeliverAll() // spread the new commit index
	for _, n := range c.Nodes {
		if n.CommitIndex != 2 {
			t.Errorf("node %d commitIndex = %d, want 2", n.ID, n.CommitIndex)
		}
	}
}
```
