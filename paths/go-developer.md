---
path: go-developer
title: Go Developer
description: Go from your first variable to building Raft — a single ordered track through the catalog's Go courses, each one building on the last.
courses:
  - go-basics
  - dsa-from-scratch
  - intro-to-concurrency
  - advanced-concurrency
  - goroutines-from-scratch
  - concurrency-gauntlet
  - raft-in-go
---

## Why this order

Every course on this track is hands-on — you read a short lesson, then make
tests pass — but each one leans on the ones before it:

1. **Go Basics** teaches the language itself: variables, slices, maps,
   pointers, structs, errors, and interfaces. Everything else assumes it.
2. **DSA from Scratch** turns that vocabulary into fluency — you implement
   the classic data structures and algorithms in plain Go, which is the
   fastest way to get comfortable with slices, pointers, and generics-free
   design.
3. **Introduction to Concurrency** starts the track's real theme:
   goroutines, channels, and how to reason about shared state.
4. **Advanced Concurrency** pushes into the patterns real services use —
   pipelines, cancellation, backpressure.
5. **Goroutines from Scratch** flips the table: you build a scheduler
   yourself, so the runtime you've been trusting stops being magic.
6. **The Concurrency Gauntlet** is deliberate practice: a battery of
   progressively nastier concurrency problems to sharpen everything so far.
7. **Raft in Go** is the capstone — a real distributed consensus protocol,
   where the language, the data structures, and the concurrency discipline
   all have to work at once.

Finish the track and you haven't just "learned Go" — you've built the kind
of systems Go was designed for.
