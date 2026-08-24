package semanticmodel

var supportedSections = [...]string{
	"manifest",
	"entities",
	"relations",
	"lifecycles",
	"invariants",
	"source_mappings",
	"full",
}

// AvailableSections returns the stable section names exposed by the model.
func AvailableSections() []string {
	return append([]string(nil), supportedSections[:]...)
}

func isSupportedSection(section string) bool {
	for _, candidate := range supportedSections {
		if section == candidate {
			return true
		}
	}
	return false
}
