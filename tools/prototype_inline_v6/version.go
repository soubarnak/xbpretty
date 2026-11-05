package main

import "strings"

// splitRevision splits an xbps version at its last '_' (the revision).
// xbps uses '_' where Debian uses '-' (#45 D13, §3.4).
func splitRevision(v string) (upstream, revision string, ok bool) {
	i := strings.LastIndex(v, "_")
	if i <= 0 || i == len(v)-1 {
		return v, "", false
	}
	return v[:i], v[i+1:], true
}

// versionDiff reproduces nala's version_diff (src/util/version.rs:3-41) with
// the xbps revision separator (#45 §3.4):
//
//  1. split both at the last '_'; if both split and the upstreams are equal,
//     emit "<upstream>_" plain + Notice(<new revision>);
//  2. otherwise split the whole version at '.', find the first differing
//     component (or where the old runs out), and wrap every new component
//     from there on in Notice;
//  3. each highlighted component closes with its own reset, so the '.' thrown
//     between two highlighted components is plain (#45 §3.4 rule 5).
func versionDiff(oldVer, newVer string, th Theme) string {
	if oldVer == "" || oldVer == newVer {
		return newVer
	}
	if oldUp, _, ok := splitRevision(oldVer); ok {
		if newUp, newRev, ok2 := splitRevision(newVer); ok2 && oldUp == newUp {
			return newUp + "_" + th.Paint(RoleNotice, newRev)
		}
	}
	oldParts := strings.Split(oldVer, ".")
	newParts := strings.Split(newVer, ".")
	start := 0
	for i := range newParts {
		if i >= len(oldParts) || oldParts[i] != newParts[i] {
			start = i
			break
		}
	}
	parts := make([]string, len(newParts))
	for i, part := range newParts {
		if i >= start {
			parts[i] = th.Paint(RoleNotice, part)
		} else {
			parts[i] = part
		}
	}
	return strings.Join(parts, ".")
}
