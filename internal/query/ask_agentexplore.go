package query

import (
	"context"
	"fmt"

	"github.com/hitzhangjie/ruminate/internal/agent"
)

// AskAgent runs the embedded ReAct explorer (see docs/109) against the same
// wiki manager and LLM provider used by Ask.
func (e *Engine) AskAgent(ctx context.Context, question string, opts *agent.Options) (*agent.Result, error) {
	if e.explorer == nil {
		return nil, fmt.Errorf("agent mode requires a real ReACT explorer")
	}
	return e.explorer.Run(ctx, question, opts)
}
