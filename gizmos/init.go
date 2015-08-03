//vi: sw=4 ts=4:
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

	Mnemonic:	globals.go
	Abstract:	package level initialisation and constants for the objects package
	Date:		18 March 2014
	Author:		E. Scott Daniels

	Mods:
*/

package gizmos


import (
	"os"
	"github.com/att/gopkgs/bleater"
)

const (
)

var (
	empty_str	string = ""					// makes &"" possible since that's not legal in go

	obj_sheep	*bleater.Bleater			// sheep that objeects have reference to when needing to bleat
)

/*
	Initialisation for the package; run once automatically at startup.
*/
func init( ) {
	obj_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater
	obj_sheep.Set_prefix( "tegu-obj" )
}

/*
	Returns the package's sheep so that the main can attach it to the
	master sheep and thus affect the volume of bleats from this package.
*/
func Get_sheep( ) ( *bleater.Bleater ) {
	return obj_sheep
}
