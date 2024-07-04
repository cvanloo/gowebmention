package webmention

type XmlPersiter struct {
	Path string
}

// *XmlPersiter implements Persiter
var _ Persister = (*XmlPersiter)(nil)

func (p *XmlPersiter) PastTargets(source URL) (pastTargets []URL, err error) {
}
