package message

import (
	"time"
)

// AppendEvent incrementally reconstructs this Msg from a streaming event.
// It accepts any event value and uses interface checks to extract data.
// Events whose ReplyID does not match this message's ID are ignored.
//
// The event package's concrete types implement the necessary accessor
// interfaces (replyIDer, eventTyper, blockIDer, etc.) so they work
// directly with this method without circular imports.
func (m *Msg) AppendEvent(ev any) {
	ri, ok := ev.(interface{ GetReplyID() string })
	if !ok || ri.GetReplyID() != m.ID {
		return
	}

	var evType string
	if et, ok := ev.(interface{ EventTypeString() string }); ok {
		evType = et.EventTypeString()
	}
	if evType == "" {
		return
	}

	switch evType {
	case "reply_end":
		m.FinishedAt = time.Now().Format(TimestampFormat)

	case "model_call_end":
		type tokenInfo interface {
			GetInputTokens() int
			GetOutputTokens() int
		}
		if te, ok := ev.(tokenInfo); ok {
			if m.Usage == nil {
				m.Usage = &Usage{}
			}
			m.Usage.InputTokens += te.GetInputTokens()
			m.Usage.OutputTokens += te.GetOutputTokens()
		}

	case "text_block_start":
		if be, ok := ev.(interface{ GetBlockID() string }); ok {
			m.Content = append(m.Content, TextBlock{Type: "text", ID: be.GetBlockID(), Text: ""})
		}

	case "text_block_delta":
		m.applyBlockDelta(ContentBlockText, ev)

	case "thinking_block_start":
		if be, ok := ev.(interface{ GetBlockID() string }); ok {
			m.Content = append(m.Content, ThinkingBlock{Type: "thinking", ID: be.GetBlockID(), Thinking: ""})
		}

	case "thinking_block_delta":
		m.applyThinkingDelta(ev)

	case "data_block_start":
		if be, ok := ev.(interface{ GetBlockID() string }); ok {
			mt := ""
			if me, ok := ev.(interface{ GetMediaType_() string }); ok {
				mt = me.GetMediaType_()
			}
			m.Content = append(m.Content, DataBlock{
				Type:   "data",
				ID:     be.GetBlockID(),
				Source: Base64Source{Type: "base64", Data: "", MediaType: mt},
			})
		}

	case "data_block_delta":
		if be, ok := ev.(interface{ GetBlockID() string }); ok {
			if de, ok := ev.(interface{ GetData() string }); ok {
				idx := m.findBlockByTypeAndID(ContentBlockData, be.GetBlockID())
				if idx >= 0 {
					if db, ok := m.Content[idx].(DataBlock); ok {
						if src, ok := db.Source.(Base64Source); ok {
							src.Data += de.GetData()
							db.Source = src
							m.Content[idx] = db
						}
					}
				}
			}
		}

	case "tool_call_start":
		if te, ok := ev.(interface{ GetToolCallID() string }); ok {
			name := ""
			if ne, ok := ev.(interface{ GetToolCallName() string }); ok {
				name = ne.GetToolCallName()
			}
			m.Content = append(m.Content, ToolCallBlock{
				Type: "tool_call", ID: te.GetToolCallID(), Name: name, Input: "", State: ToolCallPending,
			})
		}

	case "tool_call_delta":
		if te, ok := ev.(interface{ GetToolCallID() string }); ok {
			if de, ok := ev.(interface{ GetDelta() string }); ok {
				idx := m.findBlockByTypeAndID(ContentBlockToolCall, te.GetToolCallID())
				if idx >= 0 {
					if tc, ok := m.Content[idx].(ToolCallBlock); ok {
						tc.Input += de.GetDelta()
						m.Content[idx] = tc
					}
				}
			}
		}

	case "tool_result_start":
		if te, ok := ev.(interface{ GetToolCallID() string }); ok {
			name := ""
			if ne, ok := ev.(interface{ GetToolCallName() string }); ok {
				name = ne.GetToolCallName()
			}
			m.Content = append(m.Content, ToolResultBlock{
				Type: "tool_result", ID: te.GetToolCallID(), Name: name, Output: "", State: ToolResultRunning,
			})
		}

	case "tool_result_text_delta":
		if te, ok := ev.(interface{ GetToolCallID() string }); ok {
			if de, ok := ev.(interface{ GetDelta() string }); ok {
				idx := m.findBlockByTypeAndID(ContentBlockToolResult, te.GetToolCallID())
				if idx >= 0 {
					if tr, ok := m.Content[idx].(ToolResultBlock); ok {
						if s, ok := tr.Output.(string); ok {
							tr.Output = s + de.GetDelta()
						} else {
							tr.Output = de.GetDelta()
						}
						m.Content[idx] = tr
					}
				}
			}
		}

	case "tool_result_end":
		if te, ok := ev.(interface{ GetToolCallID() string }); ok {
			if se, ok := ev.(interface{ GetState() ToolResultState }); ok {
				idx := m.findBlockByTypeAndID(ContentBlockToolResult, te.GetToolCallID())
				if idx >= 0 {
					if tr, ok := m.Content[idx].(ToolResultBlock); ok {
						tr.State = se.GetState()
						m.Content[idx] = tr
					}
				}
				tcIdx := m.findBlockByTypeAndID(ContentBlockToolCall, te.GetToolCallID())
				if tcIdx >= 0 {
					if tc, ok := m.Content[tcIdx].(ToolCallBlock); ok {
						tc.State = ToolCallFinished
						m.Content[tcIdx] = tc
					}
				}
			}
		}

	case "hint_block":
		if he, ok := ev.(interface{ GetHint() string }); ok {
			blockID := ""
			if be, ok := ev.(interface{ GetBlockID() string }); ok {
				blockID = be.GetBlockID()
			}
			source := ""
			if se, ok := ev.(interface{ GetSource() string }); ok {
				source = se.GetSource()
			}
			m.Content = append(m.Content, HintBlock{
				Type: "hint", ID: blockID, Source: source, Hint: he.GetHint(),
			})
		}
	}
}

func (m *Msg) applyBlockDelta(blockType ContentBlockType, ev any) {
	be, ok := ev.(interface{ GetBlockID() string })
	if !ok {
		return
	}
	de, ok := ev.(interface{ GetDelta() string })
	if !ok {
		return
	}
	idx := m.findBlockByTypeAndID(blockType, be.GetBlockID())
	if idx < 0 {
		return
	}
	if tb, ok := m.Content[idx].(TextBlock); ok {
		tb.Text += de.GetDelta()
		m.Content[idx] = tb
	}
}

func (m *Msg) applyThinkingDelta(ev any) {
	be, ok := ev.(interface{ GetBlockID() string })
	if !ok {
		return
	}
	de, ok := ev.(interface{ GetDelta() string })
	if !ok {
		return
	}
	idx := m.findBlockByTypeAndID(ContentBlockThinking, be.GetBlockID())
	if idx < 0 {
		return
	}
	if tb, ok := m.Content[idx].(ThinkingBlock); ok {
		tb.Thinking += de.GetDelta()
		m.Content[idx] = tb
	}
}
