package providercontract

import "testing"

// TestContractWall runs the full behavior contract wall against every
// registered provider. New providers must be added here; regressions in
// usage accounting, streaming lifecycle, error taxonomy, or thinking wire
// formats fail the build gate (HARNESS_DESIGN B1).
func TestContractWall(t *testing.T) {
	harnesses := []Harness{
		OpenAIHarness(),
		AnthropicHarness(),
		DashScopeHarness(),
		GeminiHarness(),
		DeepSeekHarness(),
		MoonshotHarness(),
	}
	for i := range harnesses {
		h := &harnesses[i]
		t.Run(h.Name, func(t *testing.T) {
			Run(t, h)
		})
	}
}
