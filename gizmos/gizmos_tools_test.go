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

	Mnemonic:	gizmos_tools_test
	Abstract:	Tesets the tools
	Date:		15 Jul 2014
	Author:		E. Scott Daniels

*/

package gizmos_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/att/tegu/gizmos"
)

const (
)

/*
*/
func TestTools( t *testing.T ) {			// must use bloody camel case to be recognised by go testing


	fmt.Fprintf( os.Stderr, "\n----- tools testing begins--------\n" )
	s := "foo var1=value1 var2=val2 foo bar you"
	toks := strings.Split( s, " " )
	m := gizmos.Mixtoks2map( toks[1:], "a b c d e f" )

	for k, v := range m {
		fmt.Fprintf( os.Stderr, "%s = %s\n", k, *v )
	}
}

func test_one_hasany( t *testing.T, kstr string, ui interface{}, expect bool ) ( int ) {
	ecount := 0
	toks := strings.Split( kstr, " " )

	state := gizmos.Map_has_any( ui, toks )				// true if map has any key in the list
	if state == expect {
		fmt.Fprintf( os.Stderr, "OK:   expected %v state checking key list (tokenised): %s\n", state, kstr )
	} else {
		fmt.Fprintf( os.Stderr, "FAIL: unexpected %v state checking key list (tokenised): %s\n", state, kstr )
		t.Fail()
		ecount++
	}

	// test passing a string
	state = gizmos.Map_has_any( ui, kstr )				// true if map has any key in the list
	if state == expect {
		fmt.Fprintf( os.Stderr, "OK:   expected %v state checking key list by string: %s\n", state, kstr )
		return 0
	} else {
		fmt.Fprintf( os.Stderr, "FAIL: unexpected %v state checking key list by string: %s\n", state, kstr )
		t.Fail()
		ecount++
	}

	return ecount
}

func TestAnyKey( t *testing.T ) {
	fmt.Fprintf( os.Stderr, "\n------ key map testing ------\n" )
	m := make( map[string]bool, 15 )

	m["foo"] = true
	m["goo"] = false
	m["longer"] = false
	m["tegu_admin"] = false
	m["admin"] = false

	for k := range m {
		fmt.Fprintf( os.Stderr, "INFO: key in the map: %s\n", k )
	}

	errs := test_one_hasany( t, "foo bar now are you here", m, true )
	errs += test_one_hasany( t, "tegu_admin tegu_mirror admin", m, true )
	errs += test_one_hasany( t, "tegu_mirror tegu_bwr", m, false )

	if errs == 0 {
		fmt.Fprintf( os.Stderr, "OK:   All key checks passed\n" )
	}

	fmt.Fprintf( os.Stderr, "\n------ endkey map testing ------\n" )
}
