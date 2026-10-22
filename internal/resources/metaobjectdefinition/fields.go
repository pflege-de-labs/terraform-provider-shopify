package metaobjectdefinition

// field is a field definition as configured; the whole list is managed by Terraform.
type field struct {
	Key         string
	Type        string
	Name        string
	Description string
	Required    bool
	Validations map[string]string
}

// fieldOp is one entry of the update mutation's fieldDefinitions; exactly one member is set.
type fieldOp struct {
	Create *field
	Update *field
	Delete string
}

// diffFields turns old -> desired into update operations matched by key, plus the keys whose data gets deleted.
// Deletes come first so a type change can recreate the key; every kept field is sent in the new order,
// because resetFieldOrder puts submitted fields first.
func diffFields(old, desired []field) (ops []fieldOp, destructive []string) {
	oldByKey := make(map[string]field, len(old))
	for _, f := range old {
		oldByKey[f.Key] = f
	}
	newByKey := make(map[string]field, len(desired))
	for _, f := range desired {
		newByKey[f.Key] = f
	}

	for _, o := range old {
		n, kept := newByKey[o.Key]
		if !kept || n.Type != o.Type {
			ops = append(ops, fieldOp{Delete: o.Key})
			destructive = append(destructive, o.Key)
		}
	}
	for _, n := range desired {
		if o, ok := oldByKey[n.Key]; ok && o.Type == n.Type {
			ops = append(ops, fieldOp{Update: &n})
		} else {
			ops = append(ops, fieldOp{Create: &n})
		}
	}
	return ops, destructive
}

// duplicateKeys returns each key that appears more than once, in first-seen order.
func duplicateKeys(fields []field) []string {
	seen := map[string]int{}
	var dups []string
	for _, f := range fields {
		seen[f.Key]++
		if seen[f.Key] == 2 {
			dups = append(dups, f.Key)
		}
	}
	return dups
}
