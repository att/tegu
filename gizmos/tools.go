// vi: sw=4 ts=4:
/*
 ---------------------------------------------------------------------------
   Copyright (c) 2013-2015 AT&T Intellectual Property

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at:

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
 ---------------------------------------------------------------------------
*/


/*

	Mnemonic:	tools
	Abstract:	General functions that probably don't warrant promotion to the forge
				gopkgs library.

	Date:		10 March 2014
	Author:		E. Scott Daniels

	Mods:		13 May 2014 -- Added toks2map function.
				29 Sep 2014 -  The toks2map() funciton now stops parsing when the
				29 Sep 2014 -  The toks2map() function now stops parsing when the
					first token that does not match a key=value pair in order to
					prevent issues with the huge openstack tokens that are base64
					encoded and thus may contain tailing equal signs and look like
					a key=value when they are not.  If the the first token is
					key=value, and the openstack auth token is also a key=value
					pair, then all will be well.  In other words, the caller should
					not mix things up if values will also contain equal signs.
				26 Mar 2015 - Added support sussing out port from either ipv6 or v4 addresses.
				29 Apr 2015 - Correct bug in split port causing stack dump if nil host pointer
					passed in.
				27 May 2015 - Added Split_hpv().
				26 Aug 2015 - Added IsMAC(), IsUUID(), IsIPv4()
*/

package gizmos

import (
	"regexp"
	"strings"
	"time"

	"github.com/att/gopkgs/clike"
)


/*
	Split a string into a start/end UNIX time stamp assuming the string is in one of the
	following formats:
		+nnnn		start == now	end == now + nnn
		timestamp	start == now	end == timestamp
		ts1-ts2		start == ts1	end == ts2  (start may be adjusted to now if old)

	If the end time value is before the start time value it is set to the start time value.
*/
func Str2start_end( tok string ) ( startt int64, endt int64 ) {
	now := time.Now().Unix()

	if tok[0:1] == "+"	{
		startt = now
		endt  = startt + clike.Atoll( tok )
	} else {
		idx := strings.Index( tok, "-" )			// separate start-end times
		if idx > 0 {
			startt = clike.Atoll( tok[0:idx] )
			endt = clike.Atoll( tok[idx+1:] )

			if startt < now {
				startt = now
			}
		} else {
			startt = now
			endt = clike.Atoll( tok )
		}
	}

	if endt < startt {
		endt = startt
	}

	return
}


/*
	Split two host names of the form host1,host2 or host1-host2 and return the two strings.
*/
func Str2host1_host2( tok string ) ( h1 string, h2 string ) {

	idx := strings.Index( tok, "," )		// alternate form to allow for names like host-1
	if idx > 0 {
		h1 = tok[0:idx]
		h2 = tok[idx+1:]
	} else {
		idx = strings.Index( tok, "-" )		// separate host-host
		if idx > 0 {
			h1 = tok[0:idx]
			h2 = tok[idx+1:]
		} else {
			h1 = tok
			h2 = "any"
		}
	}

	return
}

/*
	Parse a set of tokens passed in, assuming they are name=value pairs, and generate a map.
	Parsing stops with the first token that isn't name=value and the map is returned as is.
*/
func Toks2map( toks []string ) ( m map[string]*string ) {
	m = make( map[string]*string )

	for i := range toks {
		t := strings.SplitN( toks[i], "=", 2 )

		if len( t ) == 2 {
			m[t[0]] = &t[1]
		} else {
			return
		}
	}

	return
}

/*
	Given a tegu name which is likely has the form token/project/host where host could be
	one of the combinations listed below, separate off the port value and return the
	two separate strings. Possible host format:
		ipv4
		ipv4:port
		ipv6
		[ipv6]:port
		name:port

	In the case of an IPv6 address, the brackets will be removed from the return name
	portion. It is also assumed that an ip6 address will have more than one colon as
	a part of the address regardless of whether a port is supplied.
*/
func Split_port( host *string ) ( name *string, port *string ) {
	zstr := "0"
	name = host										// default
	port = &zstr

	if host == nil {
		return
	}

	if strings.Index( *host, ":" ) < 0 {				// no port at all, not ipv6; default case
		return
	}

	if strings.Index( *host, "[" ) > 0 {							// either [ip6] or [ip6]:port
		lead_toks := strings.Split( *host, "[" )					//  result: token/proj/   ip6]:port
		trail_toks := strings.Split( lead_toks[1], "]" )   		// result: ip6   :port
		h := lead_toks[0] + trail_toks[0]
		name = &h
		if len( trail_toks ) > 1 && len( trail_toks[1] ) > 1  {
			p := trail_toks[1][1:]
			port = &p
		}

		return
	}

	fc := strings.Index( *host, ":" )				// first colon
	if fc ==  strings.LastIndex( *host, ":" ) {		// just one, then its ipv4:port or name:port
		tokens := strings.Split( *host, ":" )		// simple split will do it
		name = &tokens[0]
		port = &tokens[1]
		return
	}

	// nothing matched, then it's ip6 and no port; default case works
	return
}

/*
	Given a host string of the form: host{vlan}  where host could be:
		ipv4
		ipv4:port
		ipv6
		[ipv6]:port
		name:port
		name

	and {vlan} is optional (brackets and braces are in the syntax not
	meta syntax here), return the three components: host name, port and
	vlan. Strings are nil if missing.
*/
func Split_hpv( host *string ) ( name *string, port *string, vlan *string ) {
	v :=  ""
	vlan = &v

	tokens := strings.Split( *host, "{" )
	if len( tokens )  < 2 {						// simple case, no {vlan}
		name, port = Split_port( host )
	} else {
		name, port = Split_port( &tokens[0] )			// split out the stuff from the host portion
		v := strings.TrimRight( tokens[1], "}" )		// ditch the trailing ch assumed to be closing }
		vlan = &v
	}

	return
}


/*
	Given a host name of the form token/project/address return with the address string in
	square brackets if it seems to be an ipv6 address.
*/
func Bracket_address( oa string ) (ba *string) {

	ba = &oa
	if strings.Index( oa, ":" ) < 0 {				// not ipv6, just return pointer to it
		return
	}

	tokens := strings.SplitAfter( oa, "/" )					// keeps trailing sep for easy join
	tokens[len(tokens)-1] = "[" + tokens[len(tokens)-1]
	bs := ""
	for i := range tokens {
		bs += tokens[i]
	}
	bs += "]"

	ba = &bs

	return
}

/*
	Mixed tokens (key=value and positional) to map.
	Given an array of tokens (strings), and a list of names, generate a map of tokens
	referenced by the corresponding name.  If tokens list is longer than the name list
	the remaining tokens are not mapped.  If leading tokens are of the form key=value,
	then they are mapped directly and tokens after the last key=value pair in the tokens
	array are then mapped in order. Thus splitting the string
		action=parse verbose=true  300  usera userb
	split into tokens, and the names string of "duration u1 u2" would result in a
	map:
		{ action: "parse", verbose: true, duration: 300, u1: usera, u2: userb }

	TODO: this needs to handle quoted tokens so that "xxx = yyyy" isn't treated as key
		a value pair.
*/
func Mixtoks2map( toks []string, names string ) ( tmap map[string]*string ) {
	tmap = nil

	nlist := strings.Split( names, " " )
	maxt := len( toks )
	tmap = make( map[string]*string, len( toks ) )

	j := 0											// ensure it lives after loop
	for j = 0; j < maxt; j++ {
		if strings.Index( toks[j], "=" ) < 0 {
			break
		}

		stok := strings.SplitN( toks[j], "=", 2 )
		tmap[stok[0]] = &stok[1]
	}

	for i := range nlist {
		if j >= maxt {
			return
		}

		tmap[nlist[i]] = &toks[j]
		j++
	}

	return
}

/*
	Accepts a map and a space separated list of keys that are expected to exist in the map
	and reference non-nil or non-empty elements. Returns true when all elements in the
	list are present in the map, and false otherwise. If false is returned, a string
	listing the key(s) missing is non-empty. Map can be one of [string]string, [string]*string,
	[string]int. If int is given, than missing is true only if the key isn't in the map.

	It is not an error if the map has more than the listed elements (the function can be
	used to check for required elements etc.)
*/
func Map_has_all( mi interface{}, list  string ) ( bool, string ) {
	state := true
	missing := ""

	tokens := strings.Split( list, " " )

	switch mi.(type) {
		case map[string]string:
			m := mi.( map[string]string )
			for i := range tokens {
				v, isthere := m[tokens[i]]
				if !isthere || v == "" {
					state = false
					missing += tokens[i] + " "
				}
			}

		case map[string]*string:
			m := mi.( map[string]*string )
			for i := range tokens {
				v, isthere := m[tokens[i]]
				if !isthere || *v == "" {
					state = false
					missing += tokens[i] + " "
				}
			}

		case map[string]int:
			m := mi.( map[string]int )
			for i := range tokens {
				_, isthere := m[tokens[i]]
				if !isthere {							// assume a value of zero is acceptable, so false only if missing completely
					state = false
					missing += tokens[i] + " "
				}
			}

	}

	return state, missing
}

/*
	Accepts two string pointers and returns true if both strings are the same
	(could be pointed at different strings, true means that they are identical
	in value). If both pointers are nil, then true is returned. False otherwise.
*/
func Strings_equal( s1 *string, s2 *string ) ( bool ) {
	if s1 == nil  {
		if s2 == nil {
			return true
		}

		return false
	}

	return *s1 == *s2
}

/*
	Given a string with a list of host names, this function generates
	an array of floodlight link structures in a star arrangement as
	though wwe received the json from floodlight.

	CAUTION: we are making flood-light style links, _not_ our link object
		links.
*/

func Gen_star_topo( hosts string ) ( links []FL_link_json ) {
	toks := strings.Split( hosts, " " )

	links = make( []FL_link_json, len( toks ) )			// one link per host in the list
	for i, t := range toks {
		links[i] = FL_link_json {
			Src_switch:		"star.switch",				// switch name cannot have a dash
			Src_port:		i+1,						// port should be 1 based
			Dst_switch:		t + "@em1",
			Dst_port:		-128,
			Type:			"internal",
			Direction:		"bidirectional",
			Capacity:		10000000000,
		}
	}

	return
}

/*
	Given a pointer to string, return the string or "null". We use null so this can
	be used to generate legit json.
*/
func Safe_string( p interface{} ) ( string ) {

	sp, ok := p.( *string )
	if !ok || sp  == nil {
		return "null"
	}

	return *sp
}


/*
	Given a map[string]T where T is of known, simple, type (bool, int, int64, string, *string) return true
	if the map has any key in the array of keys passed in. This needs to support the openstack interface
	where we have an array of roles, but to make it consistent with the has_all function above,
	we'll support the keys as either a list in a string, or an array of string.
*/
func Map_has_any(  ui interface{}, ki interface{} ) (bool) {
	var (
		keys []string
	)

	switch ki.( type ) {
		case string:
			keys = strings.Split( ki.( string ), " " )

		case *string:
			keys = strings.Split( *(ki.( *string )), " " )

		case []string:
			keys = ki.( []string )

		default:
			return false						// unsupported key list type
	}

	switch m := ui.( type ) {
		case map[string]bool:
			for _, k := range keys {
				if _, ok := m[k]; ok {
					return true
				}
			}

		 case map[string]int:
			for _, k := range keys {
				if _, ok := m[k]; ok {
					return true
				}
			}

		case map[string]int64:
			for _, k := range keys {
				if _, ok := m[k]; ok {
					return true
				}
			}

		case map[string]float64:
			for _, k := range keys {
				if _, ok := m[k]; ok {
					return true
				}
			}

		case map[string]string:
			for _, k := range keys {
				if _, ok := m[k]; ok {
					return true
				}
			}

		case map[string]*string:
			for _, k := range keys {
				if _, ok := m[k]; ok {
					return true
				}
			}
	}

	return false
}

var mac_re  = regexp.MustCompile(`^([0-9a-fA-F]{1,2}:){5}[0-9a-fA-F]{1,2}$`)				// RE to match a MAC address
var uuid_re = regexp.MustCompile(`^[0-9a-fA-F]{8}-([0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$`)	// RE to match a UUID
var ipv4_re = regexp.MustCompile(`^([0-9]{1,3}\.){3}[0-9]{1,3}$`)							// RE to match an IPv4 addr

/*
	Checks if a string is a valid MAC address.
 */
func IsMAC(s string) bool {
	return mac_re.MatchString(s)
}

/*
	Checks if a string is a valid UUID.
 */
func IsUUID(s string) bool {
	return uuid_re.MatchString(s)
}

/*
	Checks if a string is a valid IPv4 address.
	Note: this regex is not entirely accurate, but is "good enough".
 */
func IsIPv4(s string) bool {
	return ipv4_re.MatchString(s)
}
