package aitest

// Claude Opus 5.5 prices in USD per million tokens, which is micro-dollars per token: $4 input, $20 output, cache
// reads $0.20 and 5-minute cache writes 1.25× input.
// ponytail: hard-coded prices for the one model; read them from config when we use more than one.
const model55 = "claude-opus-5-5"

type usage struct{ input, output, cacheRead, cacheWrite int64 }

// costMicros is what the tokens cost in micro-dollars, rounded up.
func (u usage) costMicros() int64 {
	tenths := u.input*40 + u.output*200 + u.cacheWrite*50 + u.cacheRead*2
	return (tenths + 9) / 10
}
