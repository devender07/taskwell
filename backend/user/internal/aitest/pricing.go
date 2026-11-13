package aitest

// Claude Sonnet 5.5 prices in USD per million tokens, which is micro-dollars per token: $2 input, $10 output, cache
// reads $0.20 and 5-minute cache writes 1.25× input.
// ponytail: hard-coded prices for the one model (a refusal fallback to another model is still priced as Sonnet);
// read them from config when we use more than one.
const testModel = "claude-sonnet-5-5"

type usage struct{ input, output, cacheRead, cacheWrite int64 }

// costMicros is what the tokens cost in micro-dollars, rounded up.
func (u usage) costMicros() int64 {
	tenths := u.input*20 + u.output*100 + u.cacheWrite*25 + u.cacheRead*2
	return (tenths + 9) / 10
}
