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
	//"bufio"
	//"encoding/json"
	//"flag"
	"fmt"
	//"io/ioutil"
	//"html"
	//"net/http"
	"os"
	"strings"
	//"time"
	"testing"

	"github.com/att/att/tegu/gizmos"
)

const (
)

/*
*/
func TestTools( t *testing.T ) {			// must use bloody camel case to be recognised by go testing


	fmt.Fprintf( os.Stderr, "----- tools testing begins--------\n" )
	s := "foo var1=value1 var2=val2 foo bar you"
	toks := strings.Split( s, " " )
	m := gizmos.Mixtoks2map( toks[1:], "a b c d e f" )

	for k, v := range m {
		fmt.Fprintf( os.Stderr, "%s = %s\n", k, *v )
	}
}

