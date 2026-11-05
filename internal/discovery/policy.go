package discovery

// Policy machine objects, schema 1. They share SchemaVersion with the
// package rows and are written with WriteNDJSON, so policy output stays
// compact and never HTML-escaped.

// PolicyFile is one configured repository in the global policy view. The
// precedence is 1-based `xbps-query -L` order, not xbps's package count.
type PolicyFile struct {
	Precedence  int
	Repository  string
	Signed      bool
	SignedBy    string
	Fingerprint string
}

// PinnedPackage is one held or repolocked package. Reason is `hold` or
// `repolock`.
type PinnedPackage struct {
	Name    string
	Version string
	Reason  string
}

// MachinePolicyFile is one repository object in the global machine view.
type MachinePolicyFile struct {
	Precedence  int    `json:"precedence"`
	Repository  string `json:"repository"`
	Signed      bool   `json:"signed"`
	SignedBy    string `json:"signed_by,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// MachinePinnedPackage is one pinned package object.
type MachinePinnedPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Reason  string `json:"reason"`
}

// MachineGlobalPolicy is the object `policy -m` prints without names.
type MachineGlobalPolicy struct {
	Schema         int                    `json:"schema"`
	PackageFiles   []MachinePolicyFile    `json:"package_files"`
	PinnedPackages []MachinePinnedPackage `json:"pinned_packages"`
}

// MachinePolicyRepository is one repository offering a version.
type MachinePolicyRepository struct {
	Precedence int    `json:"precedence"`
	Repository string `json:"repository"`
}

// MachinePolicyVersion is one variant in `policy -m`'s package array.
type MachinePolicyVersion struct {
	Version      string                    `json:"version"`
	Precedence   int                       `json:"precedence"`
	Installed    bool                      `json:"installed"`
	Candidate    bool                      `json:"candidate"`
	Repositories []MachinePolicyRepository `json:"repositories"`
}

// MachinePackagePolicy is one package object in `policy -m` with names.
type MachinePackagePolicy struct {
	Schema    int                    `json:"schema"`
	Package   string                 `json:"package"`
	Installed *string                `json:"installed"`
	Candidate *string                `json:"candidate"`
	Versions  []MachinePolicyVersion `json:"versions"`
}
