package market

import "slices"

// setDiff returns the sorted elements to add to old and to delete from it to get desired.
func setDiff(old, desired []string) (add, del []string) {
	add, del = []string{}, []string{}
	for _, d := range desired {
		if !slices.Contains(old, d) {
			add = append(add, d)
		}
	}
	for _, o := range old {
		if !slices.Contains(desired, o) {
			del = append(del, o)
		}
	}
	slices.Sort(add)
	slices.Sort(del)
	return add, del
}
