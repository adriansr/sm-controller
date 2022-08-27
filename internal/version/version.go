package version

var (
	version    = "unknown"
	commit     = "0000000000000000000000000000000000000000"
	buildstamp = "1970-01-01 00:00:00+00:00"
)

func Short() string {
	return version
}

func Commit() string {
	return commit
}

func Buildstamp() string {
	return buildstamp
}
