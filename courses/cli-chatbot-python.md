---
course: cli-chatbot
title: Build a CLI Chatbot
language: python
description: Build a real command-line chatbot on your own machine — the messages format, a chat loop with memory, context-window trimming, and retries + streaming.
duration_hours: 5
tags: [ai, llm]
extended_reading:
  - title: Anthropic Messages API
    url: https://platform.claude.com/docs/en/api/messages
  - title: OpenAI Chat Completions API
    url: https://platform.openai.com/docs/api-reference/chat
---

# Lesson: The Messages Format {#messages-format}

By the end of this course you will have a chatbot running in your terminal —
your API key, your machine, a real model on the other end. Each lesson adds
one piece to that app, and each challenge unit-tests the one function you
just wrote. The full app runs locally; the graded challenges never touch the
network.

Every major LLM API speaks the same basic shape: a **list of messages**, where
each message is a dict with a `role` and a `content` string.

```python
[
    {"role": "system", "content": "You are a concise assistant."},
    {"role": "user", "content": "What is a context window?"},
    {"role": "assistant", "content": "The maximum amount of text a model..."},
]
```

The three roles matter:

- **system** — instructions from *you, the developer*: persona, tone, rules.
  The user never types this; your app supplies it.
- **user** — what the human typed.
- **assistant** — what the model said back. On the next call you send it
  right back in, which is how the model "remembers" the conversation. The API
  itself is stateless — the message list *is* the memory.

Let's make one real call. Create a project folder, install the SDK, and set
your key (this course shows the Anthropic SDK; the OpenAI equivalent appears
in the next lesson and everything in the course works with either):

```bash
mkdir chatbot && cd chatbot
python -m venv .venv && source .venv/bin/activate
pip install anthropic
export ANTHROPIC_API_KEY=sk-ant-...
```

Now the first cut of `chatbot.py`:

```python
import anthropic

client = anthropic.Anthropic()  # reads ANTHROPIC_API_KEY

response = client.messages.create(
    model="claude-opus-4-8",
    max_tokens=1024,
    system="You are a concise assistant.",
    messages=[{"role": "user", "content": "Say hello in five words."}],
)
print(response.content[0].text)
```

Run it: `python chatbot.py`. That's a real model answering you.

`max_tokens` is required, and it's easy to confuse with the context window
you'll meet in the memory lesson — they cap different things. `max_tokens`
limits how long *this one reply* can be; the model stops generating,
possibly mid-sentence, once it hits that count. The context window is the
model's hard limit on *total* input size across the whole conversation.
Set `max_tokens` too low and long answers get truncated; that's independent
of how much history you've accumulated.

One wrinkle worth noticing: Anthropic's API takes the system prompt as a
separate `system=` parameter, while OpenAI's API takes it as a
`{"role": "system", ...}` message at the front of the list. We'll keep the
system message **in our history list** (the portable, OpenAI-style shape) and
let a small adapter function split it out for whichever API we call. That
adapter arrives in the next lesson.

First, the piece we can test today: every conversation in our app starts from
a helper that builds the initial history. Add this to `chatbot.py`:

```python
def make_conversation(system_prompt):
    if system_prompt is None or not system_prompt.strip():
        return []
    return [{"role": "system", "content": system_prompt.strip()}]
```

Small, but it pins down real decisions: a blank or missing prompt means an
empty history (no junk system message), stray whitespace gets stripped, and —
importantly — every call returns a *fresh list*. If two conversations shared
one list, mutating one chat's history would corrupt the other's. That kind of
aliasing bug — handing every caller the same mutable object instead of a
fresh one — is a Python classic; the tests below check for it.

## Challenge: Start the Conversation {#make-conversation points=10}

Implement `make_conversation(system_prompt)`:

- If `system_prompt` is `None`, empty, or only whitespace, return `[]`.
- Otherwise return a new list containing exactly one message:
  `{"role": "system", "content": <system_prompt with surrounding whitespace stripped>}`.
- Every call must return a fresh list — mutating one returned list must not
  affect another.

### Starter

```python
def make_conversation(system_prompt):
    # TODO: build the initial message list for a new conversation
    return None
```

### Tests

```python
from solution import make_conversation


def test_system_prompt_becomes_first_message():
    conv = make_conversation("You are a helpful assistant.")
    assert conv == [{"role": "system", "content": "You are a helpful assistant."}]


def test_no_system_prompt_means_empty_history():
    assert make_conversation(None) == []
    assert make_conversation("") == []
    assert make_conversation("   ") == []


def test_whitespace_is_stripped():
    conv = make_conversation("  Be brief.  ")
    assert conv == [{"role": "system", "content": "Be brief."}]


def test_each_call_returns_a_fresh_list():
    a = make_conversation("Be brief.")
    b = make_conversation("Be brief.")
    assert a is not b
    a.append({"role": "user", "content": "hi"})
    assert len(b) == 1
```

# Lesson: The Chat Loop {#chat-loop}

A chatbot is a loop: read input, append a user message, send the whole
history, append the reply, print it, repeat. The design question is where to
put the seam — a point in the code where you can swap in different behavior
(a fake client instead of a real one) without touching the surrounding
logic — so the loop logic is testable without the network.

The violet border marks that seam — the swappable client call — and the
dashed arrow is the loop back into the next turn.

```d2
direction: right

read: "read input" { shape: oval }
u: "append\nuser msg"
call: "client(history)\n→ reply" { style.stroke: "#a78bfa"; style.stroke-width: 2 }
a: "append reply\n& print"

read -> u -> call -> a
a -> read: "next turn" { style.stroke-dash: 4 }
```

The answer: define the turn as a function that takes the API as an argument.
A **client** in this course is just a callable — `client(messages) -> str` —
that takes the full message list and returns the assistant's reply text. In
production that callable wraps a real SDK; in tests it's a fake defined in
three lines. Add the turn function to `chatbot.py`:

```python
def chat_turn(history, user_input, client):
    history.append({"role": "user", "content": user_input})
    reply = client(history)
    history.append({"role": "assistant", "content": reply})
    return reply
```

Note the ordering: the user message goes into `history` *before* the client
is called, so the model sees the question it's answering. The reply goes in
after, so the next turn has the full exchange. `chat_turn` mutates `history`
in place and returns the reply for printing.

Now the real client. This is the adapter from the last lesson — it pulls the
system message out of our portable history shape and calls Anthropic's API:

```python
import anthropic

_anthropic = anthropic.Anthropic()

def anthropic_client(messages):
    system = ""
    if messages and messages[0]["role"] == "system":
        system = messages[0]["content"]
        messages = messages[1:]
    response = _anthropic.messages.create(
        model="claude-opus-4-8",
        max_tokens=1024,
        system=system,
        messages=messages,
    )
    return response.content[0].text
```

Prefer OpenAI? Same shape, no splitting needed because OpenAI accepts the
system message in the list (check their models page for a current model
name):

```python
from openai import OpenAI

_openai = OpenAI()  # reads OPENAI_API_KEY

def openai_client(messages):
    response = _openai.chat.completions.create(
        model="gpt-4o-mini",  # or any current chat model
        messages=messages,
    )
    return response.choices[0].message.content
```

Either callable plugs into the same `chat_turn`. Finish the app with the
loop:

```python
def main():
    history = make_conversation("You are a concise assistant.")
    print("chatbot ready — type 'quit' to exit")
    while True:
        user_input = input("> ").strip()
        if user_input in ("quit", "exit"):
            break
        if not user_input:
            continue
        print(chat_turn(history, user_input, anthropic_client))

if __name__ == "__main__":
    main()
```

Run `python chatbot.py` and have a conversation. Ask a question, then ask a
follow-up that only makes sense with memory ("shorter, please") — it works,
because the whole history rides along on every call.

The challenge below tests `chat_turn` exactly as your app uses it — but the
`client` the tests inject is a fake that records what it was called with.
That's the payoff of the seam: the same function that talks to a real model
in your terminal is verified in a sandbox with no network at all.

## Challenge: One Turn of Chat {#chat-turn points=15}

Implement `chat_turn(history, user_input, client)`:

- Append `{"role": "user", "content": user_input}` to `history` (mutate it in
  place).
- Call `client(history)` exactly once — the client must see the full history
  *including* the new user message.
- Append `{"role": "assistant", "content": <the client's return value>}` to
  `history`.
- Return the reply string.

### Starter

```python
def chat_turn(history, user_input, client):
    # TODO: append the user message, call the client, record the reply
    return None
```

### Tests

```python
from solution import chat_turn


class FakeClient:
    def __init__(self, reply):
        self.reply = reply
        self.calls = []

    def __call__(self, messages):
        self.calls.append([dict(m) for m in messages])
        return self.reply


def test_turn_appends_user_then_assistant():
    history = [{"role": "system", "content": "Be brief."}]
    client = FakeClient("Hello!")

    reply = chat_turn(history, "hi there", client)

    assert reply == "Hello!"
    assert history == [
        {"role": "system", "content": "Be brief."},
        {"role": "user", "content": "hi there"},
        {"role": "assistant", "content": "Hello!"},
    ]


def test_client_sees_full_history_including_new_user_message():
    history = [
        {"role": "system", "content": "Be brief."},
        {"role": "user", "content": "hi"},
        {"role": "assistant", "content": "Hello!"},
    ]
    client = FakeClient("It was 'hi'.")

    chat_turn(history, "what did I say first?", client)

    assert len(client.calls) == 1
    seen = client.calls[0]
    assert len(seen) == 4
    assert seen[0] == {"role": "system", "content": "Be brief."}
    assert seen[-1] == {"role": "user", "content": "what did I say first?"}


def test_history_grows_by_two_each_turn():
    history = []
    client = FakeClient("ok")
    chat_turn(history, "one", client)
    chat_turn(history, "two", client)
    assert [m["role"] for m in history] == ["user", "assistant", "user", "assistant"]
```

# Lesson: Conversation Memory {#memory}

Your chatbot now remembers everything — which is also its bug. Every turn
appends two messages, and every call re-sends the whole list. Three things
degrade as the list grows:

1. **Cost.** Input tokens are billed per call. A 100-message history is paid
   for again on *every* turn.
2. **Latency.** More input means more to process before the first output
   token appears.
3. **The context window.** Every model has a hard cap on input size. Blow
   past it and the API returns an error — your app crashes mid-conversation,
   always at the worst time, because it only happens in *long* chats.

The classic fix is trimming: before the history gets too long, drop the
oldest turns. But not the *first* message — if `history[0]` is the system
message, it carries your bot's persona and rules, and silently losing it
changes behavior in confusing ways ("why did it stop being concise after
20 minutes?"). So the policy is: **keep the system message, keep the most
recent messages, drop the middle.**

Left-to-right is history order; dashed grey is what trimming drops, and the
colored borders mark what it keeps (the oldest turns sit in the middle).

```d2
direction: right

sys: "system\n(kept)" { style.stroke: "#34d399"; style.stroke-width: 2 }

old: "dropped" {
  style.stroke-dash: 4
  u0: "user" { style.font-color: "#9ca3af" }
  a0: "asst" { style.font-color: "#9ca3af" }
}

recent: "kept" {
  style.stroke: "#a78bfa"
  style.stroke-width: 2
  u1: "user"
  a1: "asst"
}

sys -> old -> recent
```

We'll measure the budget in message count. Real apps often count tokens
instead, but the shape of the function is identical — count is simpler and
plenty for a CLI bot. Add this to `chatbot.py`:

```python
def trim_history(history, max_messages):
    if len(history) <= max_messages:
        return list(history)
    if history and history[0]["role"] == "system":
        keep = max_messages - 1
        tail = history[len(history) - keep:] if keep > 0 else []
        return [history[0]] + tail
    return history[-max_messages:]
```

It returns a *new* list rather than mutating — the caller decides what to do
with it. Wire it into your loop by reassigning after each turn:

```python
        print(chat_turn(history, user_input, anthropic_client))
        history = trim_history(history, max_messages=30)
```

Try it with a tiny budget like `max_messages=5` and watch the bot forget the
start of the conversation while keeping its persona — exactly the trade you
asked for. This is pure list logic, so the challenge needs no fake client at
all.

## Challenge: Trim the History {#trim-history points=15}

Implement `trim_history(history, max_messages)`:

- Return a **new** list; never mutate `history`.
- If `history` already fits (`len(history) <= max_messages`), return a copy
  unchanged.
- If the first message has role `"system"`, keep it plus the most recent
  `max_messages - 1` messages.
- Otherwise keep just the most recent `max_messages` messages.
- You may assume `max_messages >= 1`.

### Starter

```python
def trim_history(history, max_messages):
    # TODO: keep the system message (if any) plus the most recent messages
    return history
```

### Tests

```python
from solution import trim_history


def build(turns, system=False):
    history = []
    if system:
        history.append({"role": "system", "content": "sys"})
    for i in range(turns):
        history.append({"role": "user", "content": f"u{i}"})
        history.append({"role": "assistant", "content": f"a{i}"})
    return history


def test_short_history_is_unchanged():
    history = build(2, system=True)  # 5 messages
    assert trim_history(history, 8) == history


def test_keeps_system_and_most_recent():
    history = build(3, system=True)  # sys, u0, a0, u1, a1, u2, a2
    got = trim_history(history, 4)
    assert got == [
        {"role": "system", "content": "sys"},
        {"role": "assistant", "content": "a1"},
        {"role": "user", "content": "u2"},
        {"role": "assistant", "content": "a2"},
    ]


def test_without_system_keeps_only_most_recent():
    history = build(3)  # u0, a0, u1, a1, u2, a2
    got = trim_history(history, 2)
    assert got == [
        {"role": "user", "content": "u2"},
        {"role": "assistant", "content": "a2"},
    ]


def test_returns_a_new_list_and_leaves_input_alone():
    history = build(3, system=True)
    before = [dict(m) for m in history]
    got = trim_history(history, 3)
    assert history == before
    assert got is not history
```

# Lesson: Robustness — Retries and Streaming {#robustness}

Two upgrades separate a demo from a tool you actually use: surviving flaky
network calls, and streaming replies instead of staring at a frozen prompt.

**Retries.** API calls fail for boring reasons — a rate limit (HTTP 429), a
momentary overload (529), a dropped connection. These are *transient*: the
same request usually succeeds a second later. The fix is a tiny wrapper that
retries a callable a bounded number of times and re-raises the last error if
every attempt fails. Add it to `chatbot.py`:

```python
def call_with_retries(fn, attempts):
    for attempt in range(attempts):
        try:
            return fn()
        except Exception:
            if attempt == attempts - 1:
                raise
```

Bounded is the important word: retry forever and a real outage turns your app
into a busy-loop. Notice the wrapper catches bare `Exception` — deliberately
broad, since the tests below only care about attempt counting. A real app
should usually narrow that to the transient errors described above (the
Anthropic SDK raises typed exceptions — `RateLimitError`, `APIConnectionError`,
`InternalServerError` — for exactly this), so a bug in your own code, like a
`TypeError` from a bad dict key, fails immediately instead of quietly burning
through your retry budget.

Use it in the chat loop by wrapping the client call in a
zero-argument lambda:

```python
        reply = call_with_retries(
            lambda: chat_turn(history, user_input, anthropic_client),
            attempts=3,
        )
        print(reply)
```

Two honest footnotes for your real app. First, the official SDKs already
retry rate limits and server errors internally (the Anthropic SDK defaults to
two retries with backoff) — the wrapper still earns its keep for errors the
SDK re-raises and as the pattern you'll reuse around any flaky call. Second,
production retry loops sleep between attempts (exponential backoff) so a
struggling server isn't hammered; in your app, add `time.sleep(2 ** attempt)`
before retrying. The unit-tested version keeps sleeping out of scope — the
logic under test is the attempt counting.

**Streaming.** Long replies feel broken when they arrive all at once after
ten silent seconds. The APIs can stream text as it's generated; printing
chunks as they land makes the bot feel alive. A streaming Anthropic client
for your app:

```python
def anthropic_streaming_client(messages):
    system = ""
    if messages and messages[0]["role"] == "system":
        system = messages[0]["content"]
        messages = messages[1:]
    parts = []
    with _anthropic.messages.stream(
        model="claude-opus-4-8",
        max_tokens=1024,
        system=system,
        messages=messages,
    ) as stream:
        for text in stream.text_stream:
            print(text, end="", flush=True)
            parts.append(text)
    print()
    return "".join(parts)
```

Notice it still returns the full reply string — so it drops into `chat_turn`
unchanged, and history stays correct. Seams pay rent again: printing moved
into the client; the loop logic didn't change at all.

The challenge unit-tests the retry wrapper against a deliberately flaky fake
"client" that fails a set number of times before succeeding — the sandbox
version of a bad network day.

## Challenge: Retry Flaky Calls {#call-with-retries points=15}

Implement `call_with_retries(fn, attempts)`:

- Call `fn()` (no arguments). If it returns without raising, return its
  result immediately — no further calls.
- If `fn()` raises, try again, up to `attempts` total calls.
- If the last allowed attempt also raises, let that exception propagate.
- Do not sleep between attempts. You may assume `attempts >= 1`.

### Starter

```python
def call_with_retries(fn, attempts):
    # TODO: retry fn() on exceptions, up to `attempts` total calls
    return fn()
```

### Tests

```python
import pytest

from solution import call_with_retries


class Flaky:
    def __init__(self, failures, reply="recovered"):
        self.failures = failures
        self.calls = 0
        self.reply = reply

    def __call__(self):
        self.calls += 1
        if self.calls <= self.failures:
            raise ConnectionError(f"transient failure {self.calls}")
        return self.reply


def test_returns_immediately_on_success():
    fn = Flaky(failures=0, reply="hi")
    assert call_with_retries(fn, attempts=3) == "hi"
    assert fn.calls == 1


def test_retries_until_success():
    fn = Flaky(failures=2)
    assert call_with_retries(fn, attempts=3) == "recovered"
    assert fn.calls == 3


def test_raises_after_exhausting_attempts():
    fn = Flaky(failures=5)
    with pytest.raises(ConnectionError):
        call_with_retries(fn, attempts=2)
    assert fn.calls == 2
```

# Final Challenge: Run the Whole Conversation {#final points=50}

Your chatbot is done: it starts from a system prompt, loops through turns,
remembers the conversation, trims to a budget, and survives flaky calls. The
final challenge is the engine of that app as one function — everything you
built, with the interactive `input()` loop replaced by a list of scripted
user inputs and the real API replaced by an injected client. It doesn't
mutate `user_inputs` or return anything shared, so with a deterministic fake
client (as in the tests) it behaves like a pure function; wire in the real
`anthropic_client` and it's exactly your app's loop, network calls included.

Implement `run_conversation(user_inputs, client, system_prompt=None, max_messages=None)`:

- Start the history the way `make_conversation` does: empty if
  `system_prompt` is `None`/blank, otherwise one system message with the
  prompt stripped.
- For each string in `user_inputs`, in order, run one turn the way
  `chat_turn` does: append the user message, call `client(history)` with the
  full current history, append the assistant reply.
- After each turn, if `max_messages` is not `None`, trim the history the way
  `trim_history` does: keep a leading system message (if present) plus the
  most recent messages, at most `max_messages` total. You may assume
  `max_messages >= 3` when provided.
- Return the final history list.

### Starter

```python
def run_conversation(user_inputs, client, system_prompt=None, max_messages=None):
    # TODO: seed the history, run each turn, trim between turns
    return []
```

### Tests

```python
from solution import run_conversation


class ScriptedClient:
    def __init__(self):
        self.calls = []

    def __call__(self, messages):
        self.calls.append([dict(m) for m in messages])
        return f"reply {len(self.calls)}"


def test_runs_every_turn_in_order():
    client = ScriptedClient()
    history = run_conversation(["one", "two"], client)
    assert history == [
        {"role": "user", "content": "one"},
        {"role": "assistant", "content": "reply 1"},
        {"role": "user", "content": "two"},
        {"role": "assistant", "content": "reply 2"},
    ]


def test_system_prompt_starts_the_history():
    client = ScriptedClient()
    history = run_conversation(["hi"], client, system_prompt="  Be brief.  ")
    assert history[0] == {"role": "system", "content": "Be brief."}
    assert client.calls[0][0] == {"role": "system", "content": "Be brief."}


def test_blank_system_prompt_means_no_system_message():
    client = ScriptedClient()
    history = run_conversation(["hi"], client, system_prompt="   ")
    assert history[0] == {"role": "user", "content": "hi"}


def test_client_always_sees_current_user_message_last():
    client = ScriptedClient()
    run_conversation(["one", "two", "three"], client)
    assert len(client.calls) == 3
    for seen in client.calls:
        assert seen[-1]["role"] == "user"
    assert client.calls[1][-1] == {"role": "user", "content": "two"}


def test_trims_after_each_turn_but_keeps_system():
    client = ScriptedClient()
    history = run_conversation(
        ["one", "two", "three"], client, system_prompt="sys", max_messages=3
    )
    assert history == [
        {"role": "system", "content": "sys"},
        {"role": "user", "content": "three"},
        {"role": "assistant", "content": "reply 3"},
    ]
    # turn 3 ran on the trimmed history: system + previous turn + new user msg
    assert client.calls[2] == [
        {"role": "system", "content": "sys"},
        {"role": "user", "content": "two"},
        {"role": "assistant", "content": "reply 2"},
        {"role": "user", "content": "three"},
    ]
```
