// vi: sw=4 ts=4:

/*

	Mnemonic:	tools
	Abstract:	General functions that probably don't warrent promotion to the forge
				gopkgs library.

	Date:		10 March 2014
	Author:		E. Scott Daniels

	Mods:		13 May 2014 -- Added toks2map function.
				29 Sep 2014 -  The toks2map() funciton now stops parsing when the 
					first token that does not match a key=value pair in order to 
					prevent issues with the huge openstack tokens that are base64 
					encoded and thus may contain tailing equal signs and look like
					a key=value when they are not.  If the the first token is 
					key=value, and the openstack auth token is also a key=value
					pair, then all will be well.  In other words, the caller should
					not mix things up if values will also contain equal signs.
				26 Mar 2015 - Added support sussing out port from either ipv6 or v4 addresses.
*/

package gizmos

import (
	//"bufio"
	//"encoding/json"
	//"flag"
	//"fmt"
	//"io/ioutil"
	//"html"
	//"net/http"
	//"os"
	"strings"
	"time"

	//"codecloud.web.att.com/gopkgs/bleater"
	"codecloud.web.att.com/gopkgs/clike"
	//"codecloud.web.att.com/gopkgs/token"
	//"codecloud.web.att.com/gopkgs/ipc"
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
