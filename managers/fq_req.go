// vi: sw=4 ts=4:

/*
	Mnemonic:	fq_req.go
	Abstract:	Functions that work directly on fq_req structures.
	Date:		22 August 2014
	Author:		E. Scott Daniels

	Mods:		24 Sep 2014 : Added support for vlan id setting.
*/

package managers

import (
	"encoding/json"
	"forge.research.att.com/tegu/gizmos"
)

/*
	Create a structure that is initialised such that the defaults to acciently cause
	a match to be generated and forces output to none.
*/
func Mk_fqreq( id *string )  ( np *Fq_req ) {
	output := "none"							// table 90 fmod does not output the packet
	cookie := 0xedde

	fq_match := &Fq_parms{
		Swport:	-1,				// these defaults will not generate any match criteria
		Dscp:	-1,
		Tpsport: -1,
		Tpdport: -1,
	}

	fq_action := &Fq_parms{		// these defaults will not generate any actions
		Meta:	nil,	
		Swport:	-1,
		Dscp:	-1,
		Tpsport: -1,
		Tpdport: -1,
	}
		
	np = &Fq_req {							// fq-mgr request data
		Id:		id,
		Cookie:	cookie,
		Expiry:	10,					// default to a very short lived f-mod (DON'T defaut to 0)
		Match: 	fq_match,
		Action: fq_action,
		Table:	0,
		Output: &output,			// default to no output
		Dscp_koe: false,
	}

	return
}
/*
	Makes a deep copy (copies the match and action structs too) of a fq request
	structure. Returns a pointer to the new struct.
*/
func ( src *Fq_req ) Clone( ) ( nr *Fq_req ) {
	nmatch := &Fq_parms{}		// create new
	naction := &Fq_parms{}
	nespq := &gizmos.Spq{}
	nr = &Fq_req{}

	*nr = *src					// copy into our new struct
	*naction = *src.Action		// copy action and match to new structs
	*nmatch = *src.Match
	if src.Espq != nil {
		*nespq = *src.Espq
	} 

	nr.Espq = nespq
	nr.Match = nmatch			// must reset pointers in new to copies of match and action 
	nr.Action = naction

	return
}

/*
	Bundle the structure into json.
*/

func ( fq *Fq_req ) To_json( ) ( *string, error ) {
	jbytes, err := json.Marshal( fq )			// bundle into a json string

	s := string( jbytes )

	return &s, err
}

