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

#	Mnemonic:	ql_pass_fmods
#	Abstract:	Generates all needed flow-mods on an OVS for a passthrough reseration.
#				The passthrough reservation allows a VM to set its own DSCP markings which
#				our priority 10 flow-mod doesn't reset to 0 as the traffic goes out. 
#
#				Flow mods are set up this way:
#					inbound (none)
#
#					outbound
#						p400 Match:
#								meta == 0 &&
#								reservation VM mac
#								[potocol and port]
#							 Action:
#								mark with meta value (-M)
#
#							
#	Date:		26 January 2016
# 	Author: 	E. Scott Daniels
#
#	Mods:
# ---------------------------------------------------------------------------------------------------------

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $argv0: $@" >&2
}

function usage
{
	echo "$argv0 v1.0/11216"
	echo "usage: $argv0 -s src-mac|endpoint [-n] [-S procol:[address:]port] [-t hard-timeout]"
	echo "usage: $argv0 [-X] # delete all"
	echo "For -S address can be ipv4 or ipv6; ipv6 must be in brackets. Port should be 0 for all ports"
}

# given a string that could be mac or endpoint, convert to mac and return the mac and the 
# bridge that we found it on.  If a mac is passed in, we don't return a bridge as there 
# could be duplicate mac addresses out there (different vlans) so we can't be sure which 
# bridge the mac is on. 
function ep2mac
{
	if [[ $1 == *":"* ]]
	then
		echo "$1" ""
	fi

	typeset sw=""
	typeset mac=""
	ovs_sp2uuid -a | awk -v target="$1" '
		/^switch:/ {
			sw = $NF;
			next;
		}
		/^port: / {
			if( NF > 6 ) {
				if( target == $2 ) {
					print $5, sw;
					exit( 0 )
				}
			}
		}
	' | read mac sw

	if [[ -z $sw ]]
	then
		echo "$1" ""
	fi

	echo "$mac" "$sw"
}

# Accept proto:[address:]port and split them echoing three strings: proto, addr, port
# if address is missing the string 'none' is echoed. The global variable ip_type is set
# based on the address type and left alone if just the port was supplied.
function split_pap
{
	typeset proto=""
	typeset rest=""
	typeset addr=""

	case $1 in 					# split proto:rest
		*:\[* | *:*.*)			# proto:ipv6  or proto:ipv4
			proto=${1%%:*}
			rest=${1#*:}	
			;;

		\[*\]:* | *.*:*)		# address:port (no proto)
			proto=""
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

		*)						# assume lone udp/tcp and no :addr:port or :port
			proto=$1
			rest=""
			;;
	esac


	if [[ -z $rest ]]			# only proto given
	then
		echo "$proto none 0"
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
			echo "${proto:-none} "[${addr##*\[}]" ${rest:-0}"
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
				
			echo "$proto $addr ${rest:-0}"
			;;

		*)												# just port; default ip type, or what they set on command line
			echo "$proto none ${rest//:/}"
			;;
	esac
}

# check the current setting of ip_type and return $1 only if it's not set. If it's set
# return the current setting.  This prevents ip_type from being set by the pap function 
# and then incorrectly overridden from a comand line option.
function set_ip_type
{
	if [[ -n $ip_type ]]
	then
		echo $ip_type
	else
		echo "$1" 
	fi
}

# ----------------------------------------------------------------------------------------------------------

cookie="0x0dad"			# static for now, but might want to make them user controlled, so set them up here
bridge="br-int"

smac=""					# src mac address (local to this OVS)
forreal=""
to_value=60				# default timeout value
timout="-t $to_value"	# timeout parm given on command if -t not supplied
operation="add"			# -X sets delete action
ip_type=""				# default unless we see a specific address

while [[ $1 == -* ]]
do
	case $1 in
		-4)		ip_type="$(set_ip_type $2);;				# set if not set; don't let them override what was set in pap
		-6)		ip_type="$(set_ip_type $2);;				# set if not set; don't let them override what was set in pap
		-n)		forreal="-n";;
		-s)		smac="$2"; shift;;						# source (local) mac address (should be endpoint to be safe, but allow mac)
		-S)		split_pap "$2" | read proto sip port	# expect -S {udp|tcp}:[address:]port
echo ">>> ($proto) ($sip) ($port)"
				if [[ $sip == "none" ]]
				then
					sip=""
				else
					sip="-S $sip" 
				fi
				shift
				;;

		-t)		to_value=$2; timeout="-t $2"; shift;;	# capture both the value for math, and an option for flow-mod call
		-T)		odscp="-T $2"; shift;;
		-X)		operation="del";;

		-\?)	usage
				exit 0
				;;

		*)	echo "unrecognised option: $1"
			usage
			exit 1
			;;
	esac

	shift
done

if [[ -z $smac ]]
then
	logit "must have source endpoint (better) or mac address (unsafe) in order to generate passthrough flow-mods   [FAIL]"
	exit 1
fi

ep2mac $smac | read ep_mac ep_switch	# convert endpoint to mac, and the get bridge name (if it is an endpoint)
if [[ -n $ep_switch ]]					# it converted if switch isn't blank
then
	logit "endpoint $smac converted to mac/switch: $ep_mac/$ep_switch	[INFO]"
	smac=$ep_mac
	bridge=$ep_switch
else
	logit "$smac seems not to be an endpoint, used directly with bridge $bridge	[INFO]"
fi

if false
then
if [[ -n $sproto && -z sip ]]			# must have a source IP if source proto is supplied
then
	logit "source IP address (-S) required when prototype (-p) is supplied   [FAIL]"
	exit 1
fi
fi

if [[ $proto == "none" ]]
then
	proto=""
else
	if [[ -n $proto ]]
	then
		proto="-p $proto:$port"
	fi
fi

# the flow-mod is simple; match and set marking that causes the p10 flow-mod from matching.
set -x
send_ovs_fmod $forreal $timeout -p 400 --match -m 0x0/0x7 $sip -s $smac $proto --action  -M 0x01  -R ,0 -N $operation $cookie $bridge
rc=$(( rc + $? ))
set +x

rm -f /tmp/PID$$.*
if (( rc ))
then
	exit 1
fi

exit 0
