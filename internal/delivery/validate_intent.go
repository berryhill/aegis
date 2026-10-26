package delivery

// ValidateProjectedIntent reconstructs every field using the engine's sole
// semantic builder. The plan must come from the owner's live accepted source;
// this function validates equality, and does not grant authority.
func ValidateProjectedIntent(p Plan, i Intent) error {
	if !permits(p.Delivery, i.Operation) {
		return ErrDenied
	}
	if i.Operation == UploadBlob || i.Operation == UploadTree || i.Operation == UploadCommit {
		for _, o := range p.Objects {
			child := p
			child.Object = o
			want, err := intentFor(child, i.Operation)
			if err == nil && want == i {
				return nil
			}
		}
		return ErrDenied
	}
	want, err := intentFor(p, i.Operation)
	if err != nil || want != i {
		return ErrDenied
	}
	return nil
}
