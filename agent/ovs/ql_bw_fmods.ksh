#!/usr/bin/env ksh
# vi: ts=4 sw=4:

#	Mnemonic:	ql_bw_fmods
#	Abstract:	Generates a series of flow-mods for a bandwidth reervation.
#				These flow-mods assume rate limiting is done via the br-rl veth
#				connection to br-int and that br-int is the target OVS bridge for
#				the flow-mods. All flow-mods for a reservation are built (both inbound
#				and outbound).
#
#	Date:		20 March 2015
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
	echo "$argv0 v1.0/13205"
	echo "usage: $argv0 [-d dst-mac] [-E external-ip] [-h host] [-n] [-o] [-p|P proto:port] [-s src-mac] [-T dscp] [-t hard-timeout] [-v]"
	echo ""
}

cookie="0xface"			# static for now, but might want to make them user controlled, so set them up here
bridge="br-int"
mt_base=90				# meta table base 90 sets 0x01, 91 sets 0x02, 94 sets 0x04...
rl_port=":qosirl0"		# symbolic name for br-int side of veth to br-rl. : causes late binding trigger in send_ovs_fmod

lmac=""					# local mac 	src outbound, dest inbound
rmac=""					# remote mac	src inbound, dest outbound
vlan=""
queue=""
dscp=""
host=""
forreal=""
pri_base=0
one_switch=0
set_vlan=0
queue=""
strip_type="-T 0"		# strip dscp value on inbound packets by default (-g, global type, turns off)
timout="15"
operation="add"			# -D causes deletes

while [[ $1 == -* ]]
do
	case $1 in 
		-b)		mt_base="$2"; shift;;
		-d)		rmac="$2"; shift;;
		-D)		operation="del";;
		-E)		exip="$2"; shift;;
		-g)		strip_type="";;
		-h)		host="-h $2"; shift;;
		-n)		forreal="-n";;
		-o)		one_switch=1;;
		-p)		pri_base=5; proto="-p $2"; shift;;		# source proto:port priority must increase to match over more generic f-mods
		-P)		pri_base=5; proto="-P $2"; shift;;		# dest proto:port priority must increase to match over more generic f-mods
		-q)		aueue="-q $2"; shift;;
		-s)		lmac="$2"; shift;;
		-t)		timeout="$2"; shift;;
		-T)		dscp="-T $2"; shift;;
		-v)		set_vlan=1;;

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

if (( one_switch ))							# both VMs are attached to the same OVS
then
	echo "one_switch not supported yet" 
else										# set up just for "this" side 
	if [[ -n $exip ]]
	then
		dexip="-D $exip"					# destination external ip address is dest
		sexip="-S $exip"					# destination external ip address is source
	fi

	if [[ -z $lmac || -z $rmac ]]
	then
		logit "must have soruce and dest mac addresses in order to generate flow-mods   [FAIL]"
		exit 1
	fi

	if (( set_vlan ))
	then
		ovopt="-v $lmac"					# outbound vlan action
		ivopt="-V $lmac"					# inbound vlan action
	fi

	# inbound f-mods -- all inbound to the reservation vm must go through br-rl. all traffic inbound coming off of
	# br-rl for the vm is sent directly to the VM since outbound normal routing taints the learning switch's 
	# view of where the VM really is.
	send_ovs_fmod $forreal $host -p 500 $timeout --match -i $rl_port -d $lmac --action $ivopt $strip_type -o $lmac  $operation $cookie $bridge	# from br-rl any source dest is res vm
	rc=$(( rc + $? ))
	send_ovs_fmod $forreal $host -p 450 $timeout --match -d $lmac -s $rmac --action $queue -o $rl_port  $operation $cookie $bridge
	rc=$(( rc + $? ))
	send_ovs_fmod $forreal $host -p 425 $timeout --match -d $lmac --action -o $rl_port  $operation $cookie $bridge
	rc=$(( rc + $? ))


	# outbound f-mods - anything from the VM goes over the rate limiting bridge. anything outbound coming off of the 
	# rl bridge goes through normal processing; p450 fmod must be lower priority than the p500 rule in inbound processing
	send_ovs_fmod $forreal $host -p 450 -t 0 --match -m 0 -i $rl_port --action -R ,$mt_base -R ,0 -N	 $operation $cookie $bridge 	# anything else inbound from bridge goes out normally (persistent fmod)
	rc=$(( rc + $? ))

	send_ovs_fmod $forreal $host $timeout -p $(( 400 + pri_base )) --match -m 0x0/0x7 $dexip -s $lmac -d $rmac $proto --action $ovopt $dscp -o $rl_port $queue $operation $cookie $bridge
	rc=$(( rc + $? ))
	send_ovs_fmod $forreal $host $timeout -p 300 --match -m 0x0/0x7 -s $lmac  --action  $ovopt -T 0 -o $rl_port $operation $cookie $bridge
	rc=$(( rc + $? ))
fi

if (( rc ))
then
	exit  1
fi
exit 0
