# conway-htmx

Backend rendered Conway's Game of Life with shared playing field. Streams changes
to the frontend via HTMX-SSE.

## Roadmap

1. Implementation (done)
2. ??? (todo)
3. Profit (todo)

## Demo

**Hosted Demo:** https://stuff.985.at/htmx/conway/

To try it out on your machine, clone the repo and type `go run .`

## Evolutionary Stages

1. First, there was no SSE and the frontend polled all cells twice a second. It was
   an absolutely unusable abomination with multi-second lag.
2. Introduce SSE. Lag was still phenomenally unbearable.
3. Use deflate to compress everything. Your browser has to support deflate now. Somehow
   that fixed things and now lag was still present but ok.
4. Optimize further by only updating the changed cells. This has the added benefit of
   making the lag variable (depending on how many cells changed). At least the average
   lag was brought down significantly.