#!/usr/bin/env ksh
# vi: sw=4 ts=4:
#
# ---------------------------------------------------------------------------
#   Copyright (c) 2013-2015 AT&T Intellectual Property
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at:
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
# ---------------------------------------------------------------------------
#

#	Mnemonic:	ql_split_pap
#	Abstract:	Accept a protocol:address:port string and split it echoing the 
#				three componentes to stdout. If protocol is missing, tcp is 
#				assumed, and if port is missing 0 is assumed.  If address is missing
#				then the word "node" is emitted.  A fourth value, ip type, is also
#				echoed. 
#							
#	Date:		02 February 2016	(pd)
# 	Author: 	E. Scott Daniels
#
#	Mods:
# ---------------------------------------------------------------------------------------------------------

# Accept proto:[address:]port and split them echoing three strings: proto, addr, port
# if address is missing the string 'none' is echoed. The global variable ip_type is set
# based on the address type and left alone if just the port was supplied.

proto=""
rest=""
addr=""

while [[ $1 == -* ]]
do
	case $1 in 
		*)	echo "unrecognised option: $1"
			echo "usage: $0 [proto:][addr][:port]"
			exit 1
			;;
	esac

	shift
done

case $1 in 					# split proto:rest
	*:\[* | *:*.*)			# proto:ipv6  or proto:ipv4
		proto=${1%%:*}
		rest=${1#*:}	
		;;

	#\[*\]:* | *.*:*)		# address:port (no proto)
	\[*\] | *.*)			# address:port (no proto)
		proto="tcp"
		rest="$1"
		;;

	*::*)					# proto::port (empty address)
		proto="${1%%:*}"
		rest="${1##*:}"
		;;

	*:*)					# proto:port ('empty' address)
		proto="${1%:*}"
		rest="${1#*:}"
		;;

	*)						# assume just proto and nothing else
		proto=$1
		rest=""
		;;
esac


if [[ -z $rest ]]			# only proto given
then
	echo "${proto:-tcp} none 0"		# type is unknown, let it be blank
	return
fi

case "$rest" in 
	\[*\]*)											# ipv6 address:port or just ipv6
		addr="${rest%%]*}"
		ip_type="-6"								# must force the type; set directly, DO NOT use set_ip_type
		if [[ $rest == *"]:"* ]]
		then
			rest="${rest##*]:}"
			rest="${rest:-0}"						# handle case where : exists, but no port (implied 0)
			proto=${proto:-tcp}						# if there is a port we must ensure there is a prototype; default if not given
		else
			rest="${rest##*]}"
		fi
		echo "${proto:-tcp} "[${addr##*\[}]" ${rest:-0} $ip_type"
		;;

	*.*.*.*)										# ipv4:port or just ipv4
		ip_type="-4"
		addr="${rest%%:*}"
		if [[ $rest == *:* ]]
		then
			rest="${rest##*:}"
			proto=${proto:-tcp}						# if there is a port we must ensure there is a prototype; default if not given
		else
			rest=0
		fi
			
		echo "$proto $addr ${rest:-0} $ip_type"
		;;

	*)												# just port; default ip type, or what they set on command line
		echo "$proto none ${rest//:/} $ip_type"
		;;
esac

