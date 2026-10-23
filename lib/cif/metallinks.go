package cif

import "strings"

// metalPair is the two partners of one metalc connection row.
type metalPair struct {
	first, second compoundInstance
}

// metalLinks is what the metal rule needs from _struct_conn: whether the file
// has any metalc rows and which instances they join.
type metalLinks struct {
	hasRows bool
	pairs   []metalPair
	// overflow is set when distinct pairs ran past maxMetalLinks.
	overflow bool
}

func metalLinksFromConnections(connections []Connection) metalLinks {
	links := metalLinks{}
	for _, connection := range connections {
		if !strings.EqualFold(connection.ConnTypeID, connTypeMetalCoordination) {
			continue
		}
		links.hasRows = true
		links.pairs = append(links.pairs, metalPair{
			first:  partnerInstance(connection.Partner1),
			second: partnerInstance(connection.Partner2),
		})
	}
	return links
}

// metalLinkReader reduces _struct_conn rows to metalLinks while they are read,
// so no object is kept per row. Identical pairs are stored once.
type metalLinkReader struct {
	typeColumn int
	first      partnerColumns
	second     partnerColumns
	seen       map[metalPair]struct{}
	links      metalLinks
}

type partnerColumns struct {
	asymID, compoundID, authSeqID, insCode int
}

func newMetalLinkReader(columns []string) *metalLinkReader {
	index := func(name string) int {
		for position, candidate := range columns {
			if candidate == name {
				return position
			}
		}
		return -1
	}
	partner := func(prefix string, extraPrefix string) partnerColumns {
		return partnerColumns{
			asymID:     index(prefix + "_label_asym_id"),
			compoundID: index(prefix + "_label_comp_id"),
			authSeqID:  index(prefix + "_auth_seq_id"),
			insCode:    index(extraPrefix + "_pdb_ins_code"),
		}
	}
	return &metalLinkReader{
		typeColumn: index("conn_type_id"),
		first:      partner("ptnr1", "pdbx_ptnr1"),
		second:     partner("ptnr2", "pdbx_ptnr2"),
		seen:       map[metalPair]struct{}{},
	}
}

func (r *metalLinkReader) add(row []string) {
	if !strings.EqualFold(cell(row, r.typeColumn), connTypeMetalCoordination) {
		return
	}
	r.links.hasRows = true
	pair := metalPair{first: r.instance(row, r.first), second: r.instance(row, r.second)}
	if _, duplicate := r.seen[pair]; duplicate {
		return
	}
	if len(r.seen) >= maxMetalLinks {
		r.links.overflow = true
		return
	}
	r.seen[pair] = struct{}{}
	r.links.pairs = append(r.links.pairs, pair)
}

func (r *metalLinkReader) instance(row []string, columns partnerColumns) compoundInstance {
	return compoundInstance{
		compoundID: strings.ToUpper(cell(row, columns.compoundID)),
		asymID:     cell(row, columns.asymID),
		authSeqID:  cell(row, columns.authSeqID),
		insCode:    cell(row, columns.insCode),
	}
}
