package upayopen

import "encoding/json"

// Field is one fieldKey/fieldValue pair of a dynamic form.
type Field struct {
	Key   string `json:"fieldKey"`
	Value string `json:"fieldValue"`
}

// F builds a Field; empty values are dropped by the form builder so optional
// fields can be passed unconditionally.
func F(key, value string) Field { return Field{Key: key, Value: value} }

type formGroup struct {
	Code   string `json:"code"`
	Fields any    `json:"fields"` // []Field, or {"0": []Field} for multi groups
}

// Form assembles the formData JSON string that add/update endpoints expect:
// {"code":"person","group":[{"code":"base","fields":[{fieldKey,fieldValue}…]}…]}.
type Form struct {
	Code   string      `json:"code"`
	Groups []formGroup `json:"group"`
}

func NewForm(code string) *Form { return &Form{Code: code} }

func nonEmpty(fields []Field) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if f.Value != "" {
			out = append(out, f)
		}
	}
	return out
}

// Group adds a single-instance group.
func (f *Form) Group(code string, fields ...Field) *Form {
	if fs := nonEmpty(fields); len(fs) > 0 {
		f.Groups = append(f.Groups, formGroup{Code: code, Fields: fs})
	}
	return f
}

// MultiGroup adds one instance of a repeatable (isMultiple) group.
func (f *Form) MultiGroup(code string, fields ...Field) *Form {
	if fs := nonEmpty(fields); len(fs) > 0 {
		f.Groups = append(f.Groups, formGroup{Code: code, Fields: map[string][]Field{"0": fs}})
	}
	return f
}

// JSON returns the compact JSON string used as formData.
func (f *Form) JSON() string {
	b, _ := json.Marshal(f)
	return string(b)
}

// Info is the `info` object of payer/beneficiary/bank account detail
// responses: subject → group → fields (a list, or {"0": list} for multi groups).
type Info map[string]map[string]json.RawMessage

// Flatten returns "group.fieldKey" → value (first instance of multi groups).
func (in Info) Flatten() map[string]string {
	out := map[string]string{}
	for _, groups := range in {
		for g, raw := range groups {
			var list []Field
			if json.Unmarshal(raw, &list) != nil {
				var multi map[string][]Field
				if json.Unmarshal(raw, &multi) != nil {
					continue
				}
				list = multi["0"]
			}
			for _, f := range list {
				out[g+"."+f.Key] = f.Value
			}
		}
	}
	return out
}
