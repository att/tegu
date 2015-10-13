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

#	Mnemonic:	ql_bwow_fmods
#	Abstract:	Generates all needed flow-mods on an OVS for a oneway bandwidth reservation.
#				One way reservations mark and potentially rate limit traffic on the ingress
#				OVS only.  There is no attempt to set any flow-mods for inbound traffic as
#				we do NOT expect that the traffic has been marked by us on the way "in".
#				A oneway reservation is generally implemented when the other endpint is
#				external (cross project, or on the other side of the NAT box), and the router
#				is not a neutron router (i.e. not under OVS).
#
#				Bandwidth reservation flow mods are set up this way:
#					inbound (none)
#
#					outbound
#						p400 Match:
#								meta == 0 &&
#								openflow inport &&
#								reservation VM0 &&
#								external-IP [&& proto:port]
#							 Action:
#								mark with meta value (-M)
#								set dscp value
#								resub 0 to apply openstack fmods
#
# 				In the world of endpoints, and Tegu sending a uuid as the source endpoint, we
#				must map that uuid (neutron) to a local openflow port as the port/mac is the 
#				only way to guarentee a positive match on the traffic because macs can be duplicated.
#							
#	Date:		15 June 2015
# 	Author: 	E. Scott Daniels
#
#	Mods:		17 Jun 2015 - Corrected handling of queue value when 0.
#				09 Oct 2015 - Added ability to accept a neutron uuid for translation to mac/port.
# ---------------------------------------------------------------------------------------------------------

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $argv0: $@" >&2
}

function usage
{
	echo "$argv0 v1.0/16155"
	echo "usage: $argv0 [-6] [-d dst-uuid] [-E external-ip] [-h host] [-n] [-p|P proto:port] [-s src-uuid] [-T dscp] [-t hard-timeout]"
	echo "usage: $argv0 [-X] # delete all"
	echo ""
	echo "  -6 forces IPv6 address matching to be set"
	echo "  source and dest uuids are neutron IDs which are looked up in OVS and mapped to mac/vlan/port"
}

# accept either a uuid or mac and returns the the mac, vlan-id and openflow port associated.
# guarenteed to be correct if the uuid (neutron) is passed in, and probably correct when
# given a mac unless there are two devices with the same mac address attached to the OVS
# which seems wrong, but possible.
# Output is echoed to stdout in mac, vlan, port order.
function uuid2mac 
{
	if [[ $1 == *":"* ]]		# we assume that a uuid does not have colons
	then
		echo "$1 -1 -1"			# no vlan if mac is passed in
		return
	fi

	ql_suss_ovsd | grep $1 | awk '{ print $5, $7, $3 }'
}


# ----------------------------------------------------------------------------------------------------------

cookie="0xf00d"			# static for now, but might want to make them user controlled, so set them up here
bridge="br-int"
mt_base=90				# meta table base 90 sets 0x01, 91 sets 0x02, 94 sets 0x04...

smac=""					# src mac address (local to this OVS)
dmac=""					# dest mac (remote if not x-project)
exip=""					# external (dest) IP address (if x-project, or dest proto supplied)
queue=""
idscp=""
odscp=""
host=""
forreal=""
pri_base=0				# priority is bumpped up a bit for protocol specific f-mods
queue="0"
to_value="61"			# value used to check (without option flag)
timout="-t $to_value"	# timeout parm given on command
operation="add"			# -X sets delete action
ip_type="-4"			# default to forcing an IP type match for outbound fmods; inbound fmods do NOT use this

while [[ $1 == -* ]]
do
	case $1 in
		-6)		ip_type="-6";;							# force ip6 option to be given to send_ovs_fmod
		-d)		dmac="-d $2"; shift;;					# dest (remote) mac address (could be missing)
		-E)		exip="$2"; shift;;
		-h)		host="-h $2"; shift;;
		-n)		forreal="-n";;
		-p)		pri_base=5; sproto="-p $2"; shift;;		# source proto:port priority must increase to match over more generic f-mods
		-P)		pri_base=5; dproto="-P $2"; shift;;		# dest proto:port priority must increase to match over more generic f-mods
		-q)		queue="$2"; shift;;						# ignored until HTB replacedment is found
		#-s)		smac="$2"; shift;;						# source (local) mac address
		-s)		uuid2mac "$2" | read smac svlan sofport; shift;;
		-S)		sip="-S $2"; shift;;					# local IP needed if local port (-p) is given
		-t)		to_value=$2; timeout="-t $2"; shift;;
		-T)		odscp="-T $2"; shift;;
		-V)		match_vlan="-v $2"; shift;;
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
	logit "must have source mac address in order to generate oneway flow-mods   [FAIL]"
	exit 1
fi

match_ofport=""
if (( sofport > 0 ))			# must specifically match a port as mac may not be unique
then
	match_port="-i $sofport"
	logit "source uuid was matched and generated a port: $sofport"
fi

if [[ -n $exip ]]
then
	exip="-D $exip"
fi

if [[ -n $sproto && -z sip ]]			# must have a source IP if source proto is supplied
then
	logit "source IP address required when source prototype is supplied   [FAIL]"
	exit 1
fi

if [[ -n $dproto && -z $exip ]]
then
	logit "external (-E) ip address required when destionation prototype (-P) given    [FAIL]"
	exit 1
fi

if [[ -z $dmac && -z $exip ]]		# fail if both missing
then
	logit "must have either destination mac address or external IP address to generate oneway flow-mods; both missing   [FAIL]"
	exit 1
fi

if (( queue > 0 ))
then
	echo "-q $queue was ignored: no htb queuing allowed  [OK]"
	queue=""
	#queue="-q $queue"
else
	queue=""
fi

# CAUTION: action options to send_ovs_fmods are probably order dependent, so be careful.
set -x
send_ovs_fmod $forreal $host $timeout -p $(( 400 + pri_base )) --match $match_port $ip_type -m 0x0/0x7 $sip $exip -s $smac $dmac $dproto $sproto --action $queue $odscp -M 0x01  -R ,0 -N $operation $cookie $bridge
(( rc =  rc + $? ))
set +x

rm -f /tmp/PID$$.*
if (( rc ))
then
	exit 1
fi

exit 0
