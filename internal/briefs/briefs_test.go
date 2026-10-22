package briefs_test

import (
	"os"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/briefs"
)

// Each brief's file name names the file it was embedded from.
func TestFiles(t *testing.T) {
	for file, brief := range map[string]string{
		briefs.Stage1File:                 briefs.Stage1,
		briefs.Stage1QuoteOnceFile:        briefs.Stage1QuoteOnce,
		briefs.CheckerSupportFile:         briefs.CheckerSupport,
		briefs.CheckerReadsAloneFile:      briefs.CheckerReadsAlone,
		briefs.RepairFile:                 briefs.Repair,
		briefs.RepairEditsFile:            briefs.RepairEdits,
		briefs.RepairEditsQuoteOnceFile:   briefs.RepairEditsQuoteOnce,
		briefs.KeptLossFile:               briefs.KeptLoss,
		briefs.KeptLossEditsFile:          briefs.KeptLossEdits,
		briefs.KeptLossEditsQuoteOnceFile: briefs.KeptLossEditsQuoteOnce,
		briefs.ProposerFile:               briefs.Proposer,
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != brief {
			t.Errorf("%s is not the brief it names", file)
		}
	}
}
