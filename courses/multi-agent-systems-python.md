---
course: multi-agent-systems
title: Multi-Agent Systems
language: python
description: Build a real researcher–critic–writer multi-agent system on your own machine — role-prompted agents, sequential pipelines, a bounded critic loop, and a routing coordinator.
duration_hours: 6
tags: [ai, llm, agents]
extended_reading:
  - title: "Anthropic: Building Effective Agents"
    url: https://www.anthropic.com/engineering/building-effective-agents
  - title: LangGraph documentation
    url: https://langchain-ai.github.io/langgraph/
  - title: CrewAI documentation
    url: https://docs.crewai.com/
---

# Lesson: What Is an Agent {#what-is-an-agent}

Strip away the hype and an "agent" is three things: a **role** (a system
prompt that tells the model who it is and what it's responsible for), a
**model call**, and a **slot in a control loop** that decides when it runs
and where its output goes. That's it. LangGraph, CrewAI, AutoGen — every
framework in this space is a nicer way to arrange those three things. By the
end of this course you'll have built the arrangement yourself: a working
researcher → critic → writer team running on your machine with your API key,
and you'll know exactly what the frameworks abstract because you'll have
written the raw version of each piece.

First, the honest question: **when do multiple agents beat one big prompt?**
Not as often as the hype suggests. One model call with a good prompt is
cheaper, faster, and easier to debug — start there. Multi-agent earns its
complexity when:

- **The roles genuinely conflict.** A prompt that says "write persuasively"
  and "ruthlessly find flaws in the writing" is asking one call to hold two
  opposing stances at once. Two agents, each fully committed to its role, do
  both jobs better — the same reason a fresh code reviewer catches bugs the
  author can't see.
- **Each step needs different context.** Your researcher may pull in pages
  of source material the writer never needs to see. Splitting the work keeps
  each call's context small and focused (and cheaper).
- **You want checkable intermediate output.** A pipeline hands you every
  step's output — you can log it, test it, and see exactly where quality
  fell apart. One giant call hands you a final answer and a shrug.

When none of those hold — a summary, a classification, a single-document
Q&A — use one call. Agents multiply cost and latency; make them pay rent.

Let's build. This course reuses the conventions from the CLI chatbot course:
messages are dicts with `role` and `content`, and a **client** is a callable
`client(messages) -> str` that hides which vendor SDK you're using. Set up:

```bash
mkdir agents && cd agents
python -m venv .venv && source .venv/bin/activate
pip install anthropic
export ANTHROPIC_API_KEY=sk-ant-...
```

Create `team.py` with the same client adapter the chatbot used — it splits
a leading system message out of our portable history shape:

```python
import anthropic

_anthropic = anthropic.Anthropic()  # reads ANTHROPIC_API_KEY

def anthropic_client(messages):
    system = ""
    if messages and messages[0]["role"] == "system":
        system = messages[0]["content"]
        messages = messages[1:]
    response = _anthropic.messages.create(
        model="claude-opus-4-8",
        max_tokens=2048,
        system=system,
        messages=messages,
    )
    return response.content[0].text
```

(Prefer OpenAI? Swap in the `openai_client` from the chatbot course — every
function in this course only ever sees the `client(messages) -> str` shape.)

Now the first real piece of multi-agent machinery. An agent is a role prompt
bound to a client — which in Python is just a closure:

```python
def make_agent(name, system_prompt, client):
    def agent(task):
        messages = [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": task},
        ]
        return client(messages)
    agent.name = name
    return agent
```

Three decisions worth noticing. The message list is built **fresh on every
call** — agents here are stateless workers, not chatbots; each task arrives
with clean context (contrast with the chatbot course, where the whole point
was accumulating history). The system prompt is **prepended every time**, so
the agent can't drift out of its role. And the agent's `name` rides along as
a function attribute — plain Python, no classes needed — so logs and
transcripts can say who did what.

Make two real agents with genuinely different roles and run them:

```python
researcher = make_agent(
    "researcher",
    "You are a meticulous researcher. Given a topic, produce a compact, "
    "factual brief: key facts, numbers, names, and caveats, as bullet "
    "points. No prose, no fluff. Flag anything you are unsure about.",
    anthropic_client,
)

writer = make_agent(
    "writer",
    "You are a clear, engaging technical writer. Turn the material you "
    "are given into a short article a busy developer would enjoy. "
    "Plain language. No bullet points in the final piece.",
    anthropic_client,
)

if __name__ == "__main__":
    task = "Why do vector databases use approximate nearest neighbor search?"
    print("--- researcher ---")
    print(researcher(task))
    print("--- writer ---")
    print(writer(task))
```

Run `python team.py`. Same task, two visibly different answers — the
researcher gives you terse bullets, the writer gives you prose. That
difference is the entire trick: a role prompt turns one general model into
a specialist, and `make_agent` is the factory. In the next lesson the
researcher's output becomes the writer's input, and you have a pipeline.

The challenge below grades `make_agent` exactly as your app uses it, with
the client stubbed the same way the chatbot course stubbed it — a fake that
records what it was called with.

## Challenge: Make an Agent {#make-agent points=10}

Implement `make_agent(name, system_prompt, client)`:

- Return a callable. Calling it with a `task` string must call
  `client(messages)` exactly once, where `messages` is a **new** two-item
  list: `{"role": "system", "content": system_prompt}` followed by
  `{"role": "user", "content": task}`.
- The callable returns whatever the client returned.
- Set a `name` attribute on the returned callable equal to `name`.
- Every invocation builds a fresh message list — agents are stateless; no
  history may leak between calls.

### Starter

```python
def make_agent(name, system_prompt, client):
    # TODO: return a named callable that sends [system, user] to the client
    def agent(task):
        return None
    return agent
```

### Tests

```python
from solution import make_agent


class FakeClient:
    def __init__(self, reply="ok"):
        self.reply = reply
        self.calls = []

    def __call__(self, messages):
        self.calls.append([dict(m) for m in messages])
        return self.reply


def test_agent_sends_system_then_task():
    client = FakeClient("the pond is north of the barn")
    agent = make_agent("researcher", "You research things.", client)

    reply = agent("Where is the pond?")

    assert reply == "the pond is north of the barn"
    assert client.calls == [[
        {"role": "system", "content": "You research things."},
        {"role": "user", "content": "Where is the pond?"},
    ]]


def test_agent_has_a_name():
    agent = make_agent("writer", "You write.", FakeClient())
    assert agent.name == "writer"


def test_each_call_is_stateless():
    client = FakeClient()
    agent = make_agent("researcher", "You research.", client)

    agent("first task")
    agent("second task")

    assert len(client.calls) == 2
    # the second call must NOT contain the first task — no history leaks
    assert len(client.calls[1]) == 2
    assert client.calls[1][1] == {"role": "user", "content": "second task"}


def test_two_agents_do_not_share_state():
    client = FakeClient()
    a = make_agent("a", "Role A.", client)
    b = make_agent("b", "Role B.", client)

    a("task for a")
    b("task for b")

    assert client.calls[0][0]["content"] == "Role A."
    assert client.calls[1][0]["content"] == "Role B."
    assert a.name == "a" and b.name == "b"
```

# Lesson: Pipelines — Sequential Handoff {#pipelines}

The simplest way to compose agents is a **pipeline**: run them in order,
each agent's output becoming the next agent's input. Researcher digs up the
facts; writer turns the facts into prose. This is the workhorse pattern of
multi-agent systems — Anthropic's "Building Effective Agents" article calls
it *prompt chaining* and recommends it as the first thing to reach for,
CrewAI calls it a sequential process, and in LangGraph it's a graph whose
nodes are connected in a straight line. All of them are this function:

```python
def run_pipeline(agents, task):
    transcript = []
    current = task
    for name, agent in agents:
        current = agent(current)
        transcript.append((name, current))
    return transcript
```

`agents` is a list of `(name, agent)` pairs, so the transcript can say who
produced what. The function returns the **whole transcript**, not just the
final answer — `transcript[-1][1]` — because the intermediate steps are
where all the debugging happens. When the final article is wrong, the first
question is always "was the research wrong, or did the writer mangle good
research?" A transcript answers that in one glance; a system that only keeps
the final output can't.

Wire it into `team.py` and run a real handoff:

```python
if __name__ == "__main__":
    task = "Why do vector databases use approximate nearest neighbor search?"
    steps = run_pipeline(
        [(researcher.name, researcher), (writer.name, writer)],
        task,
    )
    for name, output in steps:
        print(f"--- {name} ---")
        print(output)
        print()
```

Run it. Read the researcher's brief, then the writer's article, and check:
did the facts survive the handoff? You've just done your first **transcript
review** — the multi-agent equivalent of reading a stack trace, and a habit
worth keeping.

Two things to notice about the handoff itself. First, the writer's system
prompt says "the material you are given" — it was written for the pipeline,
where its input is a research brief, not a raw question. Role prompts and
pipeline position are designed together. Second, everything the writer
knows arrives through one string. That's **message passing**: explicit,
loggable, testable dataflow. (The alternative — a shared scratchpad all
agents read and write — is the *blackboard* pattern; we'll weigh the
trade-offs in the last lesson.)

Try a three-stage pipeline before moving on: add a `summarizer` agent that
compresses the article to two sentences, append it to the list, run again.
Zero new plumbing — that's the payoff of the uniform `(name, agent)` shape.

In the graded version the agents are plain lambdas, because `run_pipeline`
never cares that its agents call an LLM — only that they're callables. The
tests pin down the threading (each agent sees exactly the previous output)
and the transcript shape.

## Challenge: Run the Pipeline {#run-pipeline points=15}

Implement `run_pipeline(agents, task)`, where `agents` is a list of
`(name, agent)` pairs and each `agent` is a callable taking one string and
returning one string:

- Call the first agent with `task`; call each subsequent agent with the
  previous agent's output. Each agent is called exactly once, in order.
- Return a list of `(name, output)` tuples, one per agent, in run order.
- An empty `agents` list returns `[]`.

### Starter

```python
def run_pipeline(agents, task):
    # TODO: thread the task through each agent, recording (name, output)
    return []
```

### Tests

```python
from solution import run_pipeline


def test_threads_output_into_next_agent():
    steps = run_pipeline(
        [
            ("upper", lambda text: text.upper()),
            ("exclaim", lambda text: text + "!"),
        ],
        "ducks",
    )
    assert steps == [("upper", "DUCKS"), ("exclaim", "DUCKS!")]


def test_first_agent_receives_the_task():
    seen = []

    def spy(text):
        seen.append(text)
        return "notes"

    run_pipeline([("researcher", spy)], "the original task")
    assert seen == ["the original task"]


def test_each_agent_sees_only_the_previous_output():
    inputs = []

    def stage(label):
        def agent(text):
            inputs.append((label, text))
            return label
        return agent

    run_pipeline([("a", stage("a")), ("b", stage("b")), ("c", stage("c"))], "t")
    assert inputs == [("a", "t"), ("b", "a"), ("c", "b")]


def test_single_agent_pipeline():
    assert run_pipeline([("solo", lambda t: t * 2)], "ab") == [("solo", "abab")]


def test_empty_pipeline_is_empty_transcript():
    assert run_pipeline([], "anything") == []
```

# Lesson: The Critic Loop {#critic-loop}

Pipelines only move forward. But the pattern that most improves output
quality moves **backward**: generate a draft, have a critic review it, send
it back for revision, repeat until the critic approves. Anthropic's agents
article calls this *evaluator–optimizer*; in LangGraph it's a cycle in the
graph. It works for the same reason code review works — evaluating a draft
is easier than producing one, and a reviewer who didn't write the draft
isn't attached to it.

The dashed edge back to the writer is the feedback loop; the amber note
marks the hard cap (`max_rounds`) that keeps it from ping-ponging forever.
When the budget runs out, the amber dashed exit ships the latest draft
anyway — approval is not required to terminate.

```d2
direction: right
writer: "writer\n(draft / revise)" {
  style.stroke: "#34d399"
  style.stroke-width: 2
}
critic: "critic\n(evaluate)" {
  style.stroke: "#a78bfa"
  style.stroke-width: 2
}
done: "ship draft" { shape: oval }
budget: "hard bound:\n<= max_rounds reviews" {
  shape: text
  style.font-color: "#d97706"
}
writer -> critic: "draft"
critic -> writer: "feedback" { style.stroke-dash: 4 }
critic -> done: "APPROVED"
writer -> done: "rounds exhausted:\nship latest draft" {
  style.stroke: "#d97706"
  style.stroke-dash: 4
}
budget -- critic: { style.stroke: "#d97706"; style.stroke-dash: 4 }
```

It also introduces the classic multi-agent bug. "Repeat until the critic
approves" — and what if it never approves? Critics with high standards and
writers that can't meet them will happily ping-pong forever, and unlike an
ordinary infinite loop this one **spends money on every iteration**: two
API calls per round, at real per-token prices, until you notice. Every
loop that depends on a model's judgment to terminate must carry a hard
bound. This is non-negotiable, and the graded tests below are written so
that a solution without the bound cannot pass.

First the real thing. Add a critic to `team.py`:

```python
critic = make_agent(
    "critic",
    "You are an exacting editor reviewing a draft. If the draft is "
    "accurate, clear, and complete, reply with exactly: APPROVED\n"
    "Otherwise reply with a short list of specific, actionable fixes. "
    "Do not rewrite the draft yourself.",
    anthropic_client,
)
```

The critic is still just text-in, text-out. Two small adapters turn the raw
agents into the shapes the loop needs — a verdict function that maps the
critic's reply onto approve/revise, and a writer that knows how to fold
feedback into a revision prompt:

```python
def critic_verdict(draft):
    reply = critic(f"Review this draft:\n\n{draft}")
    if reply.strip() == "APPROVED":
        return None          # approval — nothing more to say
    return reply             # feedback for the writer

def revising_writer(task, feedback):
    if feedback is None:
        return writer(task)
    return writer(
        f"{task}\n\nYour previous draft was reviewed. "
        f"Revise it to address this feedback:\n{feedback}"
    )
```

The exact-sentinel check (`APPROVED`, matched exactly) is the same move as
the `NOT_IN_CONTEXT` sentinel in the RAG course: a fixed token your code can
match mechanically beats parsing prose like "This looks pretty good to me!"
Returning `None` for approval keeps the loop's contract crisp: feedback is
a string, approval is the absence of feedback.

Now the loop itself — the graded function:

```python
def revise_until_approved(writer, critic, task, max_rounds):
    draft = writer(task, None)
    for _ in range(max_rounds):
        feedback = critic(draft)
        if feedback is None:
            return draft
        draft = writer(task, feedback)
    return draft
```

Count the calls carefully, because the tests do. One initial draft. Then at
most `max_rounds` critic reviews; each rejection buys one revision. If the
critic never approves, the loop performs exactly `max_rounds` reviews and
`max_rounds` revisions and **returns the best draft it has** — a bounded
system degrades gracefully instead of hanging. `max_rounds=3` means at most
7 API calls, a number you can put in a budget.

Wire it up and watch a revision happen:

```python
if __name__ == "__main__":
    task = "Write three sentences on why retries need exponential backoff."
    final = revise_until_approved(revising_writer, critic_verdict, task, max_rounds=3)
    print(final)
```

Add a `print` inside `critic_verdict` to see the verdicts fly past. Most
tasks get approved in a round or two; tighten the critic's standards ("be
extremely strict; approve only flawless drafts") and watch it use the whole
budget — and still terminate. That's the bound earning its keep.

In the tests, the writer and critic are stubs with call counters, and the
never-approves critic **raises if called too many times** — so a loop
without a bound doesn't hang the grader; it fails fast, loudly, the way
your bank account would have.

## Challenge: Bound the Critic Loop {#revise-until-approved points=20}

Implement `revise_until_approved(writer, critic, task, max_rounds)`:

- `writer(task, feedback)` returns a draft; `feedback` is `None` for the
  initial draft, or the critic's feedback string for a revision.
- `critic(draft)` returns `None` to approve, or a feedback string.
- Produce the initial draft with `writer(task, None)`, then review it. On
  approval, return the current draft immediately. On feedback, revise with
  `writer(task, feedback)` and review again.
- Perform at most `max_rounds` critic reviews. If the last allowed review
  still rejects, revise one final time and return that draft anyway —
  never review or revise beyond the budget.
- You may assume `max_rounds >= 1`.

### Starter

```python
def revise_until_approved(writer, critic, task, max_rounds):
    # TODO: draft, then review/revise — but never more than max_rounds reviews
    draft = writer(task, None)
    while critic(draft) is not None:
        draft = writer(task, critic(draft))
    return draft
```

### Tests

```python
from solution import revise_until_approved


class StubWriter:
    def __init__(self):
        self.calls = []

    def __call__(self, task, feedback):
        self.calls.append((task, feedback))
        return f"draft {len(self.calls)}"


class StubCritic:
    """Approves on review number `approve_on` (None = never approves).

    Raises if called more than `limit` times, so an unbounded loop fails
    fast instead of hanging the sandbox.
    """

    def __init__(self, approve_on=None, limit=10):
        self.approve_on = approve_on
        self.limit = limit
        self.calls = []

    def __call__(self, draft):
        self.calls.append(draft)
        if len(self.calls) > self.limit:
            raise RuntimeError("critic called too many times — loop is unbounded")
        if self.approve_on is not None and len(self.calls) >= self.approve_on:
            return None
        return f"feedback {len(self.calls)}"


def test_approved_on_first_review():
    writer, critic = StubWriter(), StubCritic(approve_on=1)
    result = revise_until_approved(writer, critic, "task", max_rounds=3)
    assert result == "draft 1"
    assert writer.calls == [("task", None)]
    assert critic.calls == ["draft 1"]


def test_revises_then_gets_approval():
    writer, critic = StubWriter(), StubCritic(approve_on=2)
    result = revise_until_approved(writer, critic, "task", max_rounds=5)
    assert result == "draft 2"
    assert writer.calls == [("task", None), ("task", "feedback 1")]
    assert critic.calls == ["draft 1", "draft 2"]


def test_never_approves_stops_at_max_rounds():
    writer, critic = StubWriter(), StubCritic(approve_on=None)
    result = revise_until_approved(writer, critic, "task", max_rounds=3)
    # exactly 3 reviews, each buying one revision after the initial draft
    assert len(critic.calls) == 3
    assert len(writer.calls) == 4
    assert result == "draft 4"


def test_single_round_budget():
    writer, critic = StubWriter(), StubCritic(approve_on=None)
    result = revise_until_approved(writer, critic, "task", max_rounds=1)
    assert len(critic.calls) == 1
    assert len(writer.calls) == 2
    assert result == "draft 2"
```

# Lesson: Routing — The Coordinator {#routing}

Your team has three specialists now, but every task marches through the
same fixed pipeline. Real systems add a **coordinator**: something that
looks at the incoming task and decides *which* agent (or pipeline) should
handle it. "Find sources on X" should go straight to the researcher;
"tighten this paragraph" needs only the writer; running the full
research-write-review machine on either would waste three model calls.

The coordinator (violet border) inspects one task and dispatches it to a
single handler; the dashed `default` edge is the fallback taken when no
keyword matches.

```d2
direction: right
task: "task in" { shape: oval }
router: "route()\ncoordinator" {
  style.stroke: "#a78bfa"
  style.stroke-width: 2
}
researcher: "researcher"
writer: "writer"
critic: "critic"
generalist: "generalist"
task -> router
router -> researcher: "research"
router -> writer: "write"
router -> critic: "review"
router -> generalist: "default" { style.stroke-dash: 4 }
```

Anthropic's agents article calls this pattern *routing* — classify the
incoming task, then dispatch it to a specialized handler. That classification
step splits into two standard implementations:

1. **A classifier LLM call.** Ask a model "which of these agents should
   handle this task? Reply with exactly one name." — an agent whose entire
   job is dispatch. This is what LangGraph's conditional edges usually wrap
   and how CrewAI's hierarchical process uses its manager agent.
2. **Deterministic rules.** Keyword or pattern matching on the task. No
   extra API call, no latency, no chance of the router itself
   hallucinating — and trivially testable.

Production systems very often start with (2), then graduate specific hard
cases to (1). We'll build (2) as the graded function, with (1) as a
one-liner on top of `make_agent` for your real app.

The routing table declares each agent's **capabilities** as keywords, and
the router picks the first agent whose capability appears in the task:

```python
def route(task, agents, default):
    haystack = task.lower()
    for name, keywords, agent in agents:
        if any(keyword.lower() in haystack for keyword in keywords):
            return name, agent
    return default
```

Three deliberate decisions, all of which the tests below pin down:

- **Case-insensitive matching**, both sides — users type "Research", your
  table says "research"; that must not matter.
- **First match wins, in declaration order.** When a task matches two
  agents, the table's order is the tiebreak — deterministic and visible in
  one place, instead of emergent from dict ordering or scoring.
- **A `default` you choose explicitly.** The interesting design question in
  any router is what happens when *nothing* matches. Silently dropping the
  task is a bug; crashing is rude. Declaring a fallback agent makes the
  policy explicit — ship a generalist, or an agent whose role is to ask a
  clarifying question.

Wire it into `team.py`:

```python
ROUTES = [
    ("researcher", ["research", "find", "sources", "facts"], researcher),
    ("writer", ["write", "draft", "article", "rewrite"], writer),
    ("critic", ["review", "critique", "feedback"], critic),
]

generalist = make_agent(
    "generalist",
    "You are a capable general assistant. Answer directly and concisely.",
    anthropic_client,
)

if __name__ == "__main__":
    while True:
        task = input("task> ").strip()
        if task in ("quit", "exit"):
            break
        if not task:
            continue
        name, agent = route(task, ROUTES, ("generalist", generalist))
        print(f"[routed to {name}]")
        print(agent(task))
```

Run it. Type "find sources on HNSW indexes" and watch it hit the
researcher; type "what's 2+2" and watch it fall through to the generalist.
The `[routed to ...]` line is your friend — routing bugs are invisible
without it.

And the LLM-router upgrade, when keyword rules stop being enough? It's an
agent like any other:

```python
router = make_agent(
    "router",
    "Classify the task. Reply with exactly one word — researcher, "
    "writer, critic, or generalist — naming the best agent for it.",
    anthropic_client,
)
```

Call it, match its reply against your table (with the same fallback when it
names nobody — models misspell), and you've reproduced the dispatch layer
of a hierarchical CrewAI crew. Note that `route` itself only **selects** —
it returns the agent without calling it. The caller decides what to do with
the choice: call it, log it, or route into a whole pipeline. Selection and
execution are separate concerns, and the tests enforce that separation.

## Challenge: Route the Task {#route points=15}

Implement `route(task, agents, default)`, where `agents` is a list of
`(name, keywords, agent)` triples and `default` is a `(name, agent)` pair:

- Return `(name, agent)` for the **first** entry (in list order) where any
  keyword occurs as a case-insensitive substring of `task`.
- If no entry matches, return `default`.
- Only select — never call any agent.

### Starter

```python
def route(task, agents, default):
    # TODO: first agent whose keyword appears in the task; default otherwise
    return default
```

### Tests

```python
from solution import route


def never_call(label):
    def agent(task):
        raise AssertionError(f"route must not call agents (called {label})")
    return agent


RESEARCHER = never_call("researcher")
WRITER = never_call("writer")
CRITIC = never_call("critic")

AGENTS = [
    ("researcher", ["research", "sources"], RESEARCHER),
    ("writer", ["write", "draft"], WRITER),
    ("critic", ["review", "feedback"], CRITIC),
]

DEFAULT = ("generalist", never_call("generalist"))


def test_picks_the_matching_agent():
    name, agent = route("please draft an intro paragraph", AGENTS, DEFAULT)
    assert name == "writer"
    assert agent is WRITER


def test_matching_is_case_insensitive():
    name, _ = route("RESEARCH the history of ducks", AGENTS, DEFAULT)
    assert name == "researcher"
    name, _ = route("i need Feedback on this", AGENTS, DEFAULT)
    assert name == "critic"


def test_first_match_in_declaration_order_wins():
    # matches both researcher ("sources") and writer ("write")
    name, _ = route("write up the sources", AGENTS, DEFAULT)
    assert name == "researcher"


def test_falls_back_to_default_when_nothing_matches():
    name, agent = route("what's 2+2?", AGENTS, DEFAULT)
    assert name == "generalist"
    assert agent is DEFAULT[1]


def test_never_calls_any_agent():
    # every stub raises if called; both paths must survive selection
    route("draft something", AGENTS, DEFAULT)
    route("nothing matches here", AGENTS, DEFAULT)
```

# Lesson: Shared State and What the Frameworks Give You {#shared-state}

Everything your team passes around travels **hand to hand**: `run_pipeline`
threads one string from agent to agent, the critic loop passes drafts and
feedback, the router hands one task to one agent. That's message passing,
and it has a big architectural rival: the **blackboard** (LangGraph calls
its version *graph state*, CrewAI has shared memory) — a shared store,
often just a dict, that every agent can read and write:

```python
board = {"task": task}
board["research"] = researcher(board["task"])
board["draft"] = writer(board["research"])
board["verdict"] = critic(board["draft"])
```

The trade-offs cut cleanly:

- **Message passing** makes dataflow explicit — you can read `run_pipeline`
  and know exactly what the writer sees, which is why its unit tests were
  five lines. The cost: the plumbing must anticipate what each agent needs;
  when stage 4 suddenly needs stage 1's output, you're re-threading
  signatures.
- **The blackboard** makes sharing free — any agent can look at anything,
  which fits workflows where you can't predict who needs what. The cost is
  the same as any global state: hidden coupling (which agents *actually*
  read `board["research"]`? grep and pray), ordering hazards (read a key
  before its writer ran and you get a `KeyError` at best, a silently stale
  value at worst), and — with concurrent agents — races.

A battle-tested compromise shows up in serious systems, and you already
built it: keep the handoffs as messages, and keep an **append-only
transcript** on the side. Your `run_pipeline` transcript is exactly that —
shared visibility for debugging and audit without shared *mutable* state
for logic. If you take one design rule from this lesson: reach for messages
first; adopt a blackboard when the wiring genuinely can't be predicted; and
never let agents *communicate* through state that isn't also in the
transcript, or you'll debug conversations you can't see.

**So what do LangGraph and CrewAI actually sell you?** Now you can read
their docs as a checklist of things you've written:

- **LangGraph**: your functions become *nodes*; `run_pipeline` is a chain
  of edges; the critic loop is a *cycle* with a termination condition
  (your `max_rounds` reappears as a recursion limit — the same
  infinite-loop insurance with a different name); `route` is a
  *conditional edge*; the blackboard is a typed *state* object passed to
  every node. What it adds beyond the vocabulary: checkpointing (pause and
  resume a long workflow), streaming, human-in-the-loop interrupts, and
  visualization.
- **CrewAI**: `make_agent` is an `Agent` (role, goal, backstory — a
  structured system prompt); a pipeline of tasks is a `Crew` with
  `Process.sequential`; the router-in-charge is `Process.hierarchical`
  with a manager LLM. What it adds: a library of ready-made tool
  integrations, memory, and conventions so teams stop reinventing role
  prompts.

Use them when you need what they *add* — durable long-running workflows,
their integrations, their observability. Skip them when a hundred lines of
functions you fully understand will do. Either way, you now know what's
inside the box, because you built the box.

One piece remains: assembling the whole machine — pipeline in, critic loop
on the result, bounded, with a full transcript out — as a single function.
That's the final challenge.

# Final Challenge: Run the Team {#final points=50}

Your system is complete: role-prompted specialists from `make_agent`, a
researcher → writer pipeline, a critic that bounces weak drafts back under
a hard budget, and a router in front. The final challenge is the engine of
that system as one pure function — the pipeline threading of
`run_pipeline` fused with the bounded revision of `revise_until_approved`,
producing the append-only transcript from the shared-state lesson. As
always, the LLMs arrive as injected callables; the tests stub every one.

Implement `run_team(task, pipeline, critic, max_rounds)`:

- `pipeline` is a non-empty list of `(name, agent)` pairs; each `agent`
  takes one string and returns one string. Thread `task` through the
  pipeline exactly like `run_pipeline`: first agent gets `task`, each next
  agent gets the previous output. Record each step as `(name, output)` in
  a transcript list.
- Then review: `critic(current_output)` returns `None` to approve or a
  feedback string. On approval, stop. On feedback, call the **last**
  pipeline agent with the feedback string, append the new
  `(last_agent_name, output)` step to the transcript, and review again.
- Perform at most `max_rounds` critic reviews — a never-approving critic
  gets exactly `max_rounds` reviews and `max_rounds` revisions, then the
  team ships what it has.
- Return the full transcript. The team's answer is `transcript[-1][1]`.
- You may assume `max_rounds >= 1`.

### Starter

```python
def run_team(task, pipeline, critic, max_rounds):
    # TODO: thread the pipeline, then critic-loop the last agent — bounded
    return []
```

### Tests

```python
from solution import run_team


def stage(name, log=None):
    def agent(text):
        if log is not None:
            log.append((name, text))
        return f"{name}({text})"
    return agent


class StubCritic:
    """Approves on review number `approve_on` (None = never approves).

    Raises past `limit` calls so an unbounded loop fails fast.
    """

    def __init__(self, approve_on=None, limit=10):
        self.approve_on = approve_on
        self.limit = limit
        self.calls = []

    def __call__(self, draft):
        self.calls.append(draft)
        if len(self.calls) > self.limit:
            raise RuntimeError("critic called too many times — loop is unbounded")
        if self.approve_on is not None and len(self.calls) >= self.approve_on:
            return None
        return f"feedback {len(self.calls)}"


def test_pipeline_threads_and_immediate_approval():
    critic = StubCritic(approve_on=1)
    transcript = run_team(
        "task",
        [("researcher", stage("researcher")), ("writer", stage("writer"))],
        critic,
        max_rounds=3,
    )
    assert transcript == [
        ("researcher", "researcher(task)"),
        ("writer", "writer(researcher(task))"),
    ]
    # the critic reviewed exactly the pipeline's final output, once
    assert critic.calls == ["writer(researcher(task))"]


def test_revision_reruns_only_the_last_agent():
    log = []
    critic = StubCritic(approve_on=2)
    transcript = run_team(
        "task",
        [("researcher", stage("researcher", log)), ("writer", stage("writer", log))],
        critic,
        max_rounds=5,
    )
    # one extra step by the writer, fed the critic's feedback
    assert transcript == [
        ("researcher", "researcher(task)"),
        ("writer", "writer(researcher(task))"),
        ("writer", "writer(feedback 1)"),
    ]
    # the researcher ran once; the writer ran twice
    assert [name for name, _ in log] == ["researcher", "writer", "writer"]
    assert log[2] == ("writer", "feedback 1")


def test_critic_reviews_the_current_draft_each_round():
    critic = StubCritic(approve_on=3)
    run_team("t", [("writer", stage("writer"))], critic, max_rounds=5)
    assert critic.calls == ["writer(t)", "writer(feedback 1)", "writer(feedback 2)"]


def test_never_approving_critic_is_bounded():
    critic = StubCritic(approve_on=None)
    transcript = run_team(
        "task",
        [("writer", stage("writer"))],
        critic,
        max_rounds=3,
    )
    assert len(critic.calls) == 3
    # 1 pipeline step + exactly 3 revision steps
    assert len(transcript) == 4
    assert [name for name, _ in transcript] == ["writer"] * 4
    assert transcript[-1][1] == "writer(feedback 3)"


def test_single_agent_team():
    critic = StubCritic(approve_on=1)
    transcript = run_team("hello", [("solo", stage("solo"))], critic, max_rounds=1)
    assert transcript == [("solo", "solo(hello)")]
```
