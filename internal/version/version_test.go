package version

import "testing"

func TestStringWithInjectedValues(t *testing.T) {
	oldBase, oldBranch, oldCommit, oldDirty := BaseVersion, BranchName, CommitHash, Dirty
	defer func() {
		BaseVersion, BranchName, CommitHash, Dirty = oldBase, oldBranch, oldCommit, oldDirty
	}()

	BaseVersion = "v0.0.1"
	BranchName = "feature/release test"
	CommitHash = "1234567890abcdef"
	Dirty = "true"

	got := String()
	want := "v0.0.1-feature-release-test-1234567890ab-dirty"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringWithoutDirty(t *testing.T) {
	oldBase, oldBranch, oldCommit, oldDirty := BaseVersion, BranchName, CommitHash, Dirty
	defer func() {
		BaseVersion, BranchName, CommitHash, Dirty = oldBase, oldBranch, oldCommit, oldDirty
	}()

	BaseVersion = "v1.2.3"
	BranchName = "dev"
	CommitHash = "abcdef"
	Dirty = "false"

	got := String()
	want := "v1.2.3-dev-abcdef"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringForReleaseBuildIsBare(t *testing.T) {
	oldBase, oldBranch, oldCommit, oldDirty, oldRelease := BaseVersion, BranchName, CommitHash, Dirty, Release
	defer func() {
		BaseVersion, BranchName, CommitHash, Dirty, Release = oldBase, oldBranch, oldCommit, oldDirty, oldRelease
	}()

	BaseVersion = "v0.1.0"
	BranchName = "v0.1.0"
	CommitHash = "abcdef123456"
	Dirty = "false"
	Release = "true"

	if got, want := String(), "v0.1.0"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}

	Dirty = "true"
	if got, want := String(), "v0.1.0-dirty"; got != want {
		t.Fatalf("String() with dirty tree = %q, want %q", got, want)
	}
}

func TestSanitizePart(t *testing.T) {
	got := sanitizePart(" refs/heads/dev branch! ")
	want := "refs-heads-dev-branch"
	if got != want {
		t.Fatalf("sanitizePart() = %q, want %q", got, want)
	}
}
