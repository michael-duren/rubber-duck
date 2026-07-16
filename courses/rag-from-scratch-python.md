---
course: rag-from-scratch
title: RAG from Scratch
language: python
description: Build a real retrieval-augmented generation app over your own documents — chunking, embeddings, a vector database, top-k retrieval, and grounded answers with citations.
duration_hours: 6
tags: [ai, llm, rag]
extended_reading:
  - title: sentence-transformers documentation
    url: https://sbert.net/
  - title: Chroma documentation
    url: https://docs.trychroma.com/
  - title: OpenAI Embeddings guide
    url: https://platform.openai.com/docs/guides/embeddings
---

# Lesson: Why RAG {#why-rag}

Ask a model about *your* stuff — your notes, your team's runbooks, last
month's design docs — and it will either admit it has no idea or, worse,
answer confidently and wrong. Two hard limits cause this:

1. **The model wasn't trained on your data.** Your meeting notes from last
   Tuesday are not in any training set.
2. **You can't just paste everything in.** Context windows are large but
   finite, you pay per input token on *every* call, and models get measurably
   worse at using facts buried in the middle of enormous prompts.

**Retrieval-Augmented Generation** (RAG) is the standard fix, and right now
it's one of the most practical, in-demand skills in AI engineering — nearly
every "chat with your docs" product, support bot, and internal knowledge
assistant is a RAG pipeline under the hood. The idea fits in one sentence:
*store your documents in a searchable form, and when a question arrives,
retrieve the few most relevant passages and hand only those to the model,
with instructions to answer from them alone.*

Every RAG system is the same five-stage pipeline:

```text
documents --> chunk --> embed --> store        (indexing, done once)
question  --> embed --> retrieve top-k --> prompt the model   (per query)
```

The indexing pass runs once over your corpus; ovals are the pipeline's entry
and exit, and the violet and emerald borders mark the embed step and the
vector store it fills.

```d2
direction: right
docs: "documents\n.md / .txt" {shape: oval}
chunk: "chunk"
embed: "embed" {style.stroke: "#a78bfa"; style.stroke-width: 2}
store: "vector store\n(index)" {shape: oval; style.stroke: "#34d399"; style.stroke-width: 2}
docs -> chunk -> embed -> store
```

By the end of this course that pipeline will be running on your machine, over
your own files, with real embeddings and a real vector database. Each lesson
adds one stage to the app; each challenge unit-tests the one pure function at
the heart of that stage — string and list logic, no network, no heavyweight
libraries. That split is honest: the pure pieces you'll be graded on are
exactly what the big libraries do for a living.

**Pick your corpus.** Choose a folder of your own plain-text or Markdown
files — personal notes, a blog, a project's docs directory, saved articles. A
few dozen files is plenty; anything you'd actually like to ask questions
about. Then set up the project:

```bash
mkdir ragapp && cd ragapp
python -m venv .venv && source .venv/bin/activate
pip install sentence-transformers chromadb
mkdir docs        # copy your files in here (or symlink your notes folder)
```

`sentence-transformers` downloads a small embedding model (~90 MB) on first
use and runs it locally — no API key needed. If you'd rather use a hosted
embeddings API instead, the next lessons show that variant too.

The first stage is the least glamorous: loading. Create `rag.py`:

```python
from pathlib import Path


def clean_text(text):
    return " ".join(text.split())


def load_documents(folder):
    docs = []
    for path in sorted(Path(folder).rglob("*")):
        if path.is_file() and path.suffix.lower() in {".md", ".txt"}:
            text = clean_text(path.read_text(encoding="utf-8", errors="ignore"))
            if text:
                docs.append((str(path.relative_to(folder)), text))
    return docs


if __name__ == "__main__":
    docs = load_documents("docs")
    print(f"loaded {len(docs)} documents")
    for source, text in docs[:3]:
        print(f"  {source}: {text[:60]!r}")
```

Run `python rag.py` and confirm your files show up.

Don't skip past `clean_text`. Raw files are full of formatting whitespace —
indentation, hard-wrapped lines, runs of blank lines — that carries no
meaning but eats characters. In the next lesson we slice text into
fixed-size chunks, and every wasted character is retrieval budget spent on
nothing. Every production document loader does some version of this
normalization before splitting (many text splitters strip and collapse
whitespace for exactly this reason). It's also our warm-up: small enough to
write in one line, real enough that your pipeline is already running it.

## Challenge: Clean the Text {#clean-text points=10}

Implement `clean_text(text)`:

- Collapse every run of whitespace — spaces, tabs, newlines, any mix — into a
  single space.
- Strip leading and trailing whitespace.
- A string that is empty or all whitespace becomes `""`.

### Starter

```python
def clean_text(text):
    # TODO: collapse whitespace runs to single spaces and strip the ends
    return text
```

### Tests

```python
from solution import clean_text


def test_collapses_internal_whitespace():
    assert clean_text("duck   typing\t\tis   fine") == "duck typing is fine"


def test_newlines_become_single_spaces():
    assert clean_text("line one\nline two\n\n\nline three") == "line one line two line three"


def test_strips_the_ends():
    assert clean_text("  hello world \n") == "hello world"


def test_whitespace_only_becomes_empty():
    assert clean_text("   \n\t ") == ""


def test_clean_input_is_unchanged():
    assert clean_text("already clean") == "already clean"
```

# Lesson: Chunking {#chunking}

You can't embed a whole file as one unit — well, you *can*, but retrieval
gets bad fast. An embedding is a fixed-size summary of meaning; squeeze three
unrelated topics from one long document into a single vector and it points at
none of them. And even when a big chunk *is* retrieved, you pay to stuff the
entire thing into the prompt when only one paragraph mattered.

So stage one of indexing is **chunking**: slice each document into pieces
small enough to have one meaning each, big enough to still carry context.
Two knobs control it:

- **size** — how many characters per chunk. Hundreds of characters is the
  usual ballpark: a paragraph or two.
- **overlap** — how many characters each chunk shares with the previous one.
  Without overlap, a sentence that straddles a boundary gets cut in half and
  neither half embeds well; with it, the boundary region appears intact in
  both neighbors.

Overlap must be *smaller* than size — otherwise a chunk starts at or before
where the previous one started and the loop never advances. Your function
should reject that loudly (`ValueError`), because it's a config bug, not a
data condition.

This is precisely what the fancy splitters in production frameworks do —
LangChain's `RecursiveCharacterTextSplitter`, LlamaIndex's node parsers.
Theirs try to cut on paragraph and sentence boundaries before falling back
to raw characters, but the core loop — *walk the text, emit `size`-character
windows, step forward by `size - overlap`* — is exactly the function you're
about to write. Add it to `rag.py`:

```python
def chunk_text(text, size, overlap):
    if size <= 0:
        raise ValueError("size must be positive")
    if overlap < 0 or overlap >= size:
        raise ValueError("overlap must be >= 0 and smaller than size")
    if not text:
        return []
    chunks = []
    start = 0
    while start < len(text):
        chunks.append(text[start:start + size])
        if start + size >= len(text):
            break
        start += size - overlap
    return chunks
```

The `break` matters: once a chunk reaches the end of the text, stop. A naive
`range(0, len(text), step)` loop happily emits one more chunk that lies
entirely inside the previous one — pure duplication that pollutes retrieval
(the tests below catch that bug specifically).

Now chunk your real corpus. Each chunk gets an id like `notes.md#3` — source
file plus chunk index — which is what your app will cite in its answers
later. Add:

```python
def build_corpus(docs, size=800, overlap=200):
    corpus = []
    for source, text in docs:
        for i, chunk in enumerate(chunk_text(text, size, overlap)):
            corpus.append((f"{source}#{i}", chunk))
    return corpus


if __name__ == "__main__":
    docs = load_documents("docs")
    corpus = build_corpus(docs)
    print(f"{len(docs)} documents -> {len(corpus)} chunks")
    for chunk_id, chunk in corpus[:3]:
        print(f"  {chunk_id}: {chunk[:60]!r}")
```

Run it. Eyeball a few chunks: do they read like coherent passages? Try
`size=200` and watch them turn into confetti; try `size=5000` and watch
whole files collapse into single chunks. There's no universal right answer —
this knob is one of the highest-leverage tuning parameters in any real RAG
system, and now you own it.

## Challenge: Chunk the Text {#chunk-text points=15}

Implement `chunk_text(text, size, overlap)`:

- Raise `ValueError` if `size <= 0`, if `overlap < 0`, or if
  `overlap >= size`.
- Return `[]` for empty text.
- Otherwise return consecutive slices of `text`, each at most `size`
  characters, where each chunk after the first starts `size - overlap`
  characters after the previous chunk's start.
- Stop as soon as a chunk reaches the end of the text — never emit a chunk
  that lies entirely inside the previous one.

### Starter

```python
def chunk_text(text, size, overlap):
    # TODO: slice text into overlapping chunks of at most `size` characters
    return [text]
```

### Tests

```python
import pytest

from solution import chunk_text


def test_short_text_is_one_chunk():
    assert chunk_text("tiny", size=100, overlap=20) == ["tiny"]


def test_exact_fit_is_one_chunk():
    assert chunk_text("abcde", size=5, overlap=2) == ["abcde"]


def test_empty_text_has_no_chunks():
    assert chunk_text("", size=100, overlap=20) == []


def test_no_overlap_splits_on_exact_boundaries():
    assert chunk_text("abcdefghij", size=5, overlap=0) == ["abcde", "fghij"]


def test_overlap_repeats_the_boundary_region():
    # step = size - overlap = 3, and the final chunk ends exactly at the end
    assert chunk_text("abcdefgh", size=5, overlap=2) == ["abcde", "defgh"]


def test_covers_the_whole_text_in_order():
    text = "The quick brown fox jumps over the lazy dog. " * 20
    chunks = chunk_text(text, size=100, overlap=30)
    assert chunks[0] == text[:100]
    assert all(len(chunk) <= 100 for chunk in chunks)
    # dropping each chunk's 30-char overlap and stitching rebuilds the text
    rebuilt = chunks[0] + "".join(chunk[30:] for chunk in chunks[1:])
    assert rebuilt == text


def test_overlap_must_be_smaller_than_size():
    with pytest.raises(ValueError):
        chunk_text("abc", size=5, overlap=5)
    with pytest.raises(ValueError):
        chunk_text("abc", size=5, overlap=7)


def test_size_positive_and_overlap_nonnegative():
    with pytest.raises(ValueError):
        chunk_text("abc", size=0, overlap=0)
    with pytest.raises(ValueError):
        chunk_text("abc", size=5, overlap=-1)
```

# Lesson: Embeddings and Similarity {#embeddings}

An **embedding** turns text into a vector — a plain list of floats, a few
hundred long — with one magic property: texts that *mean* similar things get
vectors that *point* in similar directions. "How do I reset my password?"
and "steps to recover account access" share almost no words, but their
embeddings are near-parallel. That property is the entire trick behind
semantic search; everything else in RAG is plumbing around it.

Add local embeddings to `rag.py` with sentence-transformers:

```python
from sentence_transformers import SentenceTransformer

_model = SentenceTransformer("all-MiniLM-L6-v2")


def embed_texts(texts):
    return _model.encode(texts, normalize_embeddings=True).tolist()
```

`all-MiniLM-L6-v2` maps each string to 384 floats — that's the "few hundred
long" vector made concrete. It also has an input limit: text past 256 word
pieces gets truncated, and a truncated chunk embeds only its first part. This
is why the 800-character default from the chunking lesson isn't arbitrary —
it comfortably fits under that limit with room to spare, so every chunk gets
embedded in full instead of silently losing its tail. Swap in a model with a
shorter limit (or hand it much bigger chunks) and this stops being true —
worth checking whenever you change either knob.

Prefer a hosted embeddings API? Same shape, swap the body (and
`pip install openai`, `export OPENAI_API_KEY=...`):

```python
from openai import OpenAI

_openai = OpenAI()  # reads OPENAI_API_KEY

def embed_texts(texts):
    resp = _openai.embeddings.create(model="text-embedding-3-small", input=texts)
    return [item.embedding for item in resp.data]
```

Either way, `embed_texts` takes a list of strings and returns a list of
vectors — the rest of the course doesn't care which one you picked. That's
the same injectable-callable seam you used for the LLM client in the CLI
chatbot course, and it's what will keep the graded challenges offline.

How do we compare two vectors? **Cosine similarity**: the cosine of the
angle between them.

```text
cos(a, b) = (a . b) / (|a| * |b|)
```

Dot product over the product of lengths. It ranges from 1.0 (same
direction — same meaning), through 0.0 (unrelated), to -1.0 (opposite).
Dividing by the lengths makes it care only about *direction*, so a long
document and a three-word query can still match. Add it to `rag.py`:

```python
import math


def cosine_similarity(a, b):
    dot = sum(x * y for x, y in zip(a, b))
    norm_a = math.sqrt(sum(x * x for x in a))
    norm_b = math.sqrt(sum(y * y for y in b))
    if norm_a == 0 or norm_b == 0:
        return 0.0
    return dot / (norm_a * norm_b)
```

(The zero-vector guard avoids a `ZeroDivisionError` on degenerate input; by
convention a zero vector is similar to nothing.)

Now the payoff — semantic search over your own corpus, brute force:

```python
if __name__ == "__main__":
    corpus = build_corpus(load_documents("docs"))
    vectors = embed_texts([chunk for _, chunk in corpus])

    query = "how do I restore a backup"   # ask something YOUR docs answer
    qvec = embed_texts([query])[0]

    scored = [
        (cosine_similarity(qvec, vec), chunk_id, chunk)
        for (chunk_id, chunk), vec in zip(corpus, vectors)
    ]
    scored.sort(reverse=True)
    for score, chunk_id, chunk in scored[:5]:
        print(f"{score:.3f}  {chunk_id}  {chunk[:70]!r}")
```

Run it a few times with different queries. Spend real time here — this is
the eyeball test every RAG engineer runs constantly. Do the top hits
actually answer the question? Try a query using words that appear nowhere in
your docs but mean the same thing as something that does; watch it match
anyway. That's embeddings earning their keep.

One note before the challenge: `normalize_embeddings=True` asked the model
for unit-length vectors, which makes cosine similarity collapse to a plain
dot product. Vector databases lean on that constantly. Your graded function
takes arbitrary vectors, so it does the full formula — the one line every
vector database on earth evaluates a billion times a day. No stub needed;
it's pure arithmetic.

## Challenge: Cosine Similarity {#cosine-similarity points=15}

Implement `cosine_similarity(a, b)` for two equal-length lists of numbers:

- Return the dot product of `a` and `b` divided by the product of their
  Euclidean lengths.
- If either vector has length zero (all zeros), return `0.0`.
- Plain Python lists of ints or floats must work — no numpy.

### Starter

```python
def cosine_similarity(a, b):
    # TODO: dot(a, b) / (|a| * |b|), with a zero-vector guard
    return 0.0
```

### Tests

```python
import math

import pytest

from solution import cosine_similarity


def test_same_direction_is_one():
    assert cosine_similarity([1.0, 2.0, 3.0], [1.0, 2.0, 3.0]) == pytest.approx(1.0)


def test_scaling_does_not_change_similarity():
    assert cosine_similarity([1.0, 2.0, 3.0], [10.0, 20.0, 30.0]) == pytest.approx(1.0)


def test_orthogonal_is_zero():
    assert cosine_similarity([1.0, 0.0], [0.0, 1.0]) == pytest.approx(0.0)


def test_opposite_is_minus_one():
    assert cosine_similarity([1.0, 2.0], [-1.0, -2.0]) == pytest.approx(-1.0)


def test_forty_five_degrees():
    assert cosine_similarity([1.0, 0.0], [1.0, 1.0]) == pytest.approx(math.sqrt(2) / 2)


def test_zero_vector_scores_zero():
    assert cosine_similarity([0.0, 0.0], [1.0, 2.0]) == 0.0
    assert cosine_similarity([1.0, 2.0], [0.0, 0.0]) == 0.0


def test_plain_int_lists_work():
    assert cosine_similarity([3, 4], [3, 4]) == pytest.approx(1.0)
```

# Lesson: Retrieval and the Vector Database {#retrieval}

Last lesson's brute-force loop *is* retrieval — score every chunk, take the
best. So why does every RAG stack ship a vector database? Three reasons:

1. **Persistence.** Embedding your corpus costs time (or API dollars). A DB
   stores the vectors so you index once and query forever; the brute-force
   script re-embedded everything on every run.
2. **Scale.** Scoring all vectors is fine at 1,000 chunks and hopeless at
   100 million. Vector DBs build approximate-nearest-neighbor indexes (HNSW
   graphs, IVF cells) that find the near-best matches while touching a tiny
   fraction of the data.
3. **Filters and metadata.** "Top 5 chunks, but only from docs tagged
   `runbook`, modified this year" — databases are good at that part.

We'll use **Chroma** because it's an embedded library — no server to run,
the index is just a folder. FAISS (a raw index library, closer to the metal)
and Qdrant (a client-server DB you talk to over HTTP) occupy the same seat
in the pipeline; everything below maps one-to-one. Add to `rag.py`:

```python
import chromadb

_db = chromadb.PersistentClient(path="./index")
_collection = _db.get_or_create_collection(
    "docs", metadata={"hnsw:space": "cosine"}
)


def index_corpus(corpus, vectors):
    _collection.add(
        ids=[chunk_id for chunk_id, _ in corpus],
        documents=[chunk for _, chunk in corpus],
        embeddings=vectors,
    )


def retrieve(question, k=4):
    qvec = embed_texts([question])[0]
    res = _collection.query(query_embeddings=[qvec], n_results=k)
    return list(zip(res["ids"][0], res["documents"][0], res["distances"][0]))
```

Two details worth understanding rather than cargo-culting:

- `hnsw:space: cosine` tells Chroma to rank by cosine; its default is
  squared L2 distance. Rule one of vector search: know which metric your
  index uses.
- Chroma returns cosine **distance** (`1 - similarity`, lower is better),
  not similarity. `score = 1.0 - distance` converts back.

Index your corpus and query it:

```python
if __name__ == "__main__":
    if _collection.count() == 0:
        corpus = build_corpus(load_documents("docs"))
        index_corpus(corpus, embed_texts([chunk for _, chunk in corpus]))
        print(f"indexed {_collection.count()} chunks")

    for chunk_id, chunk, dist in retrieve("how do I restore a backup"):
        print(f"{1.0 - dist:.3f}  {chunk_id}  {chunk[:70]!r}")
```

First run indexes; every run after that starts instantly from the `./index`
folder — persistence, reason one, felt firsthand. The scores should match
your brute-force numbers from last lesson, because **`.query()` is not
magic**: score the query vector against stored vectors, return the k best,
with an index structure so it doesn't have to touch all of them. Strip away
the acceleration and what remains is one small pure function — score
everything, sort descending, slice. That's your challenge, and when you can
write it, `n_results=4` stops being an incantation.

Two behaviors your version should pin down (the real ones do too): asking
for more results than exist returns what exists, and **ties are stable** —
equal scores keep their original order instead of shuffling between runs.
Determinism makes retrieval debuggable.

## Challenge: Top K {#top-k points=15}

Implement `top_k(query_vec, entries, k)`, where `entries` is a list of
`(id, vector)` tuples. A working `cosine_similarity` is provided in the
starter — this challenge is about the selection logic:

- Score every entry: `cosine_similarity(query_vec, vector)`.
- Return a list of `(id, score)` tuples sorted by score, highest first.
- Entries with equal scores keep their original input order (stable sort).
- Return at most `k` results; fewer if there aren't `k` entries.
- If `k <= 0`, return `[]`.

### Starter

```python
import math


def cosine_similarity(a, b):
    dot = sum(x * y for x, y in zip(a, b))
    norm_a = math.sqrt(sum(x * x for x in a))
    norm_b = math.sqrt(sum(y * y for y in b))
    if norm_a == 0 or norm_b == 0:
        return 0.0
    return dot / (norm_a * norm_b)


def top_k(query_vec, entries, k):
    # TODO: score every entry, sort by score descending (stable), keep k
    return [(entry_id, 0.0) for entry_id, _ in entries][:k]
```

### Tests

```python
import pytest

from solution import top_k

ENTRIES = [
    ("readme#0", [0.0, 1.0]),
    ("notes#0", [1.0, 0.0]),
    ("notes#1", [0.6, 0.8]),
    ("blog#0", [-1.0, 0.0]),
]


def test_ranks_by_similarity_to_the_query():
    got = top_k([1.0, 0.0], ENTRIES, k=4)
    assert [entry_id for entry_id, _ in got] == ["notes#0", "notes#1", "readme#0", "blog#0"]


def test_scores_are_cosine_similarities():
    got = dict(top_k([1.0, 0.0], ENTRIES, k=4))
    assert got["notes#0"] == pytest.approx(1.0)
    assert got["notes#1"] == pytest.approx(0.6)
    assert got["readme#0"] == pytest.approx(0.0)
    assert got["blog#0"] == pytest.approx(-1.0)


def test_returns_at_most_k():
    got = top_k([1.0, 0.0], ENTRIES, k=2)
    assert [entry_id for entry_id, _ in got] == ["notes#0", "notes#1"]


def test_k_larger_than_entries_returns_everything():
    assert len(top_k([1.0, 0.0], ENTRIES, k=99)) == 4


def test_equal_scores_keep_input_order():
    entries = [
        ("first", [1.0, 0.0]),
        ("second", [2.0, 0.0]),  # same direction -> identical cosine
        ("third", [0.0, 1.0]),
    ]
    got = top_k([1.0, 0.0], entries, k=2)
    assert [entry_id for entry_id, _ in got] == ["first", "second"]


def test_nonpositive_k_is_empty():
    assert top_k([1.0, 0.0], ENTRIES, k=0) == []
    assert top_k([1.0, 0.0], ENTRIES, k=-3) == []


def test_empty_entries_is_empty():
    assert top_k([1.0, 0.0], [], k=5) == []
```

# Lesson: Grounded Answering {#grounded-answering}

Everything so far finds the right passages. The last stage hands them to a
model in a way that keeps it honest. Three rules do most of the work:

1. **Answer only from the sources.** The retrieved chunks go into the
   prompt, with an explicit instruction to use nothing else.
2. **Cite.** Each chunk already has an id (`notes.md#2`). Making the model
   cite ids turns "trust me" into "check `notes.md#2`" — and makes retrieval
   failures visible when a claimed citation doesn't say what the answer
   claims.
3. **Refuse honestly.** Two failure modes, two guards. If even the *best*
   retrieval score is weak, the corpus doesn't cover the question — refuse
   *before* spending tokens on an LLM call. If the scores looked fine but
   the passages still don't answer it, the prompt instructs the model to
   say a fixed sentinel — `NOT_IN_CONTEXT` — instead of improvising. A
   sentinel beats prose ("Unfortunately I could not…") because your code
   can match it exactly and translate it to a UX-appropriate message.

Here is the whole per-query path those rules govern. The dashed edges are the
two lookups — the question is embedded, then matched against the index by
similarity; the solid arrows are the answer path, and the colored borders
mark retrieval, the LLM call, and the grounded answer.

```d2
direction: right
q: "question" {shape: oval}
retrieve: "retrieve\ntop-k" {style.stroke: "#22d3ee"; style.stroke-width: 2}
idx: "vector\nindex"
prompt: "build\nprompt"
llm: "LLM" {style.stroke: "#a78bfa"; style.stroke-width: 2}
ans: "answer\n+ cites" {shape: oval; style.stroke: "#34d399"; style.stroke-width: 2}
q -> retrieve: "embed" {style.stroke-dash: 4}
idx -> retrieve: "similarity" {style.stroke-dash: 4}
retrieve -> prompt -> llm -> ans
```

Prompt assembly is plain string building, and the exact format is the
graded function. Add it to `rag.py`:

```python
def build_prompt(question, hits):
    lines = [
        "Answer the question using only the sources below.",
        "Cite every source you use by its bracketed id, like [notes.md#2].",
        "If the sources do not answer the question, reply exactly: NOT_IN_CONTEXT",
        "",
    ]
    for source, text in hits:
        lines.append(f"[{source}] {text}")
        lines.append("")
    lines.append(f"Question: {question}")
    return "\n".join(lines)
```

Instructions first, then one `[id] text` block per hit with blank lines
between, then the question last (models weight the end of a prompt heavily —
finish on the task, not on source #7). This is the same job LangChain's
"stuff documents" chain or LlamaIndex's response synthesizer does: format
retrieved context into a template. Frameworks make it configurable; the
string logic is this.

Now the LLM client — the same injectable-callable seam as the CLI chatbot
course, simplified for this course's shape. There, `client(messages) -> str`
took the full conversation history, because each turn had to see everything
said before. Here there's no multi-turn memory to replay — every question
gets one assembled prompt and one answer — so the callable narrows to
`client(prompt) -> str`, a single string in, a string back
(`pip install anthropic`, or adapt to the OpenAI SDK the same way as in that
course):

```python
import anthropic

_anthropic = anthropic.Anthropic()  # reads ANTHROPIC_API_KEY


def llm_client(prompt):
    response = _anthropic.messages.create(
        model="claude-opus-4-8",
        max_tokens=1024,
        messages=[{"role": "user", "content": prompt}],
    )
    return response.content[0].text
```

Close the loop:

```python
REFUSAL = "I couldn't find anything about that in your documents."


def ask(question, k=4, min_score=0.25):
    hits = []
    for chunk_id, chunk, dist in retrieve(question, k):
        if 1.0 - dist >= min_score:          # cosine distance -> similarity
            hits.append((chunk_id, chunk))
    if not hits:
        return REFUSAL                        # guard 1: weak retrieval
    reply = llm_client(build_prompt(question, hits))
    if reply.strip() == "NOT_IN_CONTEXT":
        return REFUSAL                        # guard 2: model saw nothing useful
    return reply


if __name__ == "__main__":
    print("rag ready — type 'quit' to exit")
    while True:
        question = input("ask> ").strip()
        if question in ("quit", "exit"):
            break
        if question:
            print(ask(question))
```

Run it. Ask something your documents answer and check the citations point at
real chunks. Then ask something they *don't* answer ("who won the 1998
World Cup?") and watch it refuse instead of hallucinating. That refusal is
the difference between a demo and a system you can put in front of users.
`min_score` is a knob you must tune against your own corpus and embedding
model — try a few queries and watch the scores; there is no universal
threshold.

The challenge grades `build_prompt` character-for-character. Pedantic? It's
the point: prompts are interfaces. In production you'll diff prompts across
versions, cache on their hashes, and write regression tests asserting a
retrieved chunk landed in the right slot. "Roughly the right string" isn't a
spec.

## Challenge: Build the Prompt {#build-prompt points=15}

Implement `build_prompt(question, hits)`, where `hits` is a non-empty list
of `(source, text)` tuples. Return exactly this layout, with single `\n`
newlines (no trailing newline):

```text
Answer the question using only the sources below.
Cite every source you use by its bracketed id, like [notes.md#2].
If the sources do not answer the question, reply exactly: NOT_IN_CONTEXT

[<source>] <text>

[<source>] <text>

Question: <question>
```

One `[<source>] <text>` line per hit, in order, each followed by a blank
line; the question line is last.

### Starter

```python
def build_prompt(question, hits):
    # TODO: instructions, then one [source] text block per hit, then the question
    return question
```

### Tests

```python
from solution import build_prompt

HEADER = (
    "Answer the question using only the sources below.\n"
    "Cite every source you use by its bracketed id, like [notes.md#2].\n"
    "If the sources do not answer the question, reply exactly: NOT_IN_CONTEXT\n"
)


def test_exact_prompt_for_two_hits():
    prompt = build_prompt(
        "What do ducks eat?",
        [
            ("notes.md#0", "Ducks eat plants and insects."),
            ("pond.md#3", "Feeding bread to ducks is discouraged."),
        ],
    )
    assert prompt == (
        HEADER
        + "\n"
        + "[notes.md#0] Ducks eat plants and insects.\n"
        + "\n"
        + "[pond.md#3] Feeding bread to ducks is discouraged.\n"
        + "\n"
        + "Question: What do ducks eat?"
    )


def test_sources_appear_in_hit_order():
    prompt = build_prompt("q", [("a#1", "alpha"), ("b#2", "beta"), ("c#3", "gamma")])
    assert prompt.index("[a#1] alpha") < prompt.index("[b#2] beta") < prompt.index("[c#3] gamma")


def test_question_comes_last():
    prompt = build_prompt("Where is the pond?", [("a#1", "alpha")])
    assert prompt.endswith("Question: Where is the pond?")


def test_single_hit_appears_once():
    prompt = build_prompt("q", [("doc.md#0", "the only source")])
    assert prompt.count("[doc.md#0] the only source") == 1
```

# Final Challenge: Ask Your Documents {#final points=50}

Your app is complete: it loads and chunks your files, embeds them, indexes
them in a real vector database, retrieves by meaning, and answers with
citations — or refuses. The final challenge is that app's query path as one
pure function. Everything you built is in it: cosine scoring, top-k
selection, the score gate, prompt assembly. The two impure edges — the
embedding model and the LLM — arrive as injected callables, exactly as
`embed_texts` and `llm_client` do in `rag.py`; the tests stub them the same
way the CLI chatbot course stubbed its client.

Implement
`retrieve_and_answer(question, entries, embed, client, k=3, min_score=0.25)`:

- `entries` is a list of `(chunk_id, text, vector)` tuples — your indexed
  corpus.
- Call `embed(question)` exactly once to get the query vector.
- Score every entry with cosine similarity (a working `cosine_similarity` is
  in the starter), and select up to `k` best entries — highest score first,
  ties keeping input order, exactly like your `top_k`.
- Drop selected entries scoring below `min_score`. If nothing survives (or
  `entries` is empty), return `None` **without calling `client`** — that's
  the refusal gate.
- Otherwise build the prompt with your `build_prompt` format from the
  previous lesson, using the surviving `(chunk_id, text)` pairs in score
  order, call `client(prompt)` exactly once, and return its reply.

### Starter

```python
import math


def cosine_similarity(a, b):
    dot = sum(x * y for x, y in zip(a, b))
    norm_a = math.sqrt(sum(x * x for x in a))
    norm_b = math.sqrt(sum(y * y for y in b))
    if norm_a == 0 or norm_b == 0:
        return 0.0
    return dot / (norm_a * norm_b)


def retrieve_and_answer(question, entries, embed, client, k=3, min_score=0.25):
    # TODO: embed the question, rank the entries, refuse on weak evidence,
    # build the prompt, call the client once, return its reply
    return None
```

### Tests

```python
from solution import retrieve_and_answer


class FakeEmbed:
    def __init__(self, vector):
        self.vector = vector
        self.calls = []

    def __call__(self, text):
        self.calls.append(text)
        return list(self.vector)


class FakeClient:
    def __init__(self, reply="Ducks eat plants and insects [notes.md#0]."):
        self.reply = reply
        self.prompts = []

    def __call__(self, prompt):
        self.prompts.append(prompt)
        return self.reply


ENTRIES = [
    ("notes.md#0", "Ducks eat plants and insects.", [1.0, 0.0]),
    ("db.md#0", "Postgres uses MVCC for concurrency.", [0.0, 1.0]),
    ("pond.md#1", "Ducks are common on the pond in spring.", [0.8, 0.6]),
]


def test_returns_the_client_reply():
    embed = FakeEmbed([1.0, 0.0])
    client = FakeClient()
    answer = retrieve_and_answer("What do ducks eat?", ENTRIES, embed, client)
    assert answer == "Ducks eat plants and insects [notes.md#0]."
    assert embed.calls == ["What do ducks eat?"]
    assert len(client.prompts) == 1


def test_prompt_has_top_hits_in_score_order_and_question_last():
    embed = FakeEmbed([1.0, 0.0])
    client = FakeClient()
    retrieve_and_answer("What do ducks eat?", ENTRIES, embed, client, k=2)
    prompt = client.prompts[0]
    assert "[notes.md#0] Ducks eat plants and insects." in prompt
    assert "[pond.md#1] Ducks are common on the pond in spring." in prompt
    assert prompt.index("[notes.md#0]") < prompt.index("[pond.md#1]")
    assert "Postgres" not in prompt
    assert prompt.endswith("Question: What do ducks eat?")


def test_low_scoring_hits_are_dropped_from_the_prompt():
    embed = FakeEmbed([1.0, 0.0])
    client = FakeClient()
    retrieve_and_answer("What do ducks eat?", ENTRIES, embed, client, k=3, min_score=0.5)
    prompt = client.prompts[0]
    # db.md#0 scores 0.0 — inside the top 3, but below min_score
    assert "Postgres" not in prompt
    assert "[notes.md#0]" in prompt
    assert "[pond.md#1]" in prompt


def test_refuses_without_calling_the_client_on_weak_scores():
    embed = FakeEmbed([0.0, 1.0])
    client = FakeClient()
    entries = [("notes.md#0", "Ducks eat plants and insects.", [1.0, 0.0])]
    answer = retrieve_and_answer("What is MVCC?", entries, embed, client, min_score=0.25)
    assert answer is None
    assert client.prompts == []


def test_refuses_on_an_empty_corpus():
    embed = FakeEmbed([1.0, 0.0])
    client = FakeClient()
    assert retrieve_and_answer("anything", [], embed, client) is None
    assert client.prompts == []


def test_k_limits_how_many_sources_are_used():
    embed = FakeEmbed([1.0, 0.0])
    client = FakeClient()
    retrieve_and_answer("What do ducks eat?", ENTRIES, embed, client, k=1, min_score=0.0)
    prompt = client.prompts[0]
    assert "[notes.md#0]" in prompt
    assert "pond.md#1" not in prompt
    assert "Postgres" not in prompt
```
