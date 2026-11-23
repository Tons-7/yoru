package transcode

const tailKeep = 64 << 10

type tailBuffer struct {
	kept []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n >= tailKeep {
		t.kept = append(t.kept[:0], p[n-tailKeep:]...)
		return n, nil
	}
	t.kept = append(t.kept, p...)
	if len(t.kept) > 2*tailKeep {
		t.kept = append(t.kept[:0], t.kept[len(t.kept)-tailKeep:]...)
	}
	return n, nil
}

func (t *tailBuffer) String() string {
	if len(t.kept) > tailKeep {
		return string(t.kept[len(t.kept)-tailKeep:])
	}
	return string(t.kept)
}
