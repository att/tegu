#!/bin/ksh
#
#                            AT&T - PROPRIETARY
#              THIS FILE CONTAINS PROPRIETARY INFORMATION OF
#            AT&T AND IS NOT TO BE DISCLOSED OR USED EXCEPT IN
#                  ACCORDANCE WITH APPLICABLE AGREEMENTS.
#
#                         Copyright (c) 2015 AT&T
#                   Unpublished and Not for Publication
#                          All Rights Reserved
#
#       Name:      tegu_add_mirror
#       Usage:     tegu_add_mirror [-v] <name> <port1>[,<port2>...] <output> [<vlan>]
#       Abstract:  This script starts a mirror named <name> on openvswitch.
#
#                  The port list for the mirror is named by <port1>, <port2>, etc. which
#                  must be a comma-separated list of ports that already exist on br-int.
#                  The ports can be named either by a UUID or MAC.  If a MAC is provided,
#                  this script translates to a UUID.
#
#                  <output> directs where the output of the mirror goes.  There are three possibilities:
#                  1. If <output> is vlan:nnn such that 1 <= n <= 4095, it is the VLAN
#                  number for the output VLAN.
#                  2. If <output> is an IPv4 address, then a port is created that acts as
#                  one end of a GRE tunnel to the IP address.
#                  3. If <output> is a UUID (or MAC) of an existing port on br-int, then output is
#                  directed to that port.
#
#                  If <vlan> (optional) is specified, and is a comma-separated list of VLAN
#                  IDs, it is used to select the VLANs whose traffic should be mirrored.
#                  That is, a "select-vlan=$vlan" is added to the call to openvswitch
#
#                  The -v switch causes all openvswitch commands to be echoed.
#
#                  If succesful, this command prints the mirror name on exit.
#
#       Author:    Robert Eby
#       Date:      4 February 2015
#
#       Mods:      4 Feb 2015 - created
#

function valid_ip
{
	echo "$1." | grep -E -o "([0-9]{1,3}[\.]){4}$"
	return $?
}

function valid_port
{
	for t in $brports
	do
		[ "$1" == "$t" ] && return 0
	done
	return 1
}

function translatemac
{
	ovs_sp2uuid -a | awk -v mac=$1 '/^port/ && $5 == mac { print $2 }'
}

# Preliminaries
PATH=/sbin:/usr/bin:/bin
echo=:
if [ "$1" == "-v" ]
then
	shift
	echo=echo
fi
if [ $# -lt 3 -o $# -gt 4 ]
then
	echo "usage: tegu_add_mirror [-v] name port1[,port2,...] output [vlan]" >&2
	exit 1
fi
if [ ! -x /usr/bin/ovs-vsctl ]
then
	echo "tegu_add_mirror: ovs-vsctl is not installed or not executable." >&2
	exit 2
fi

bridgename=br-int		# bridge will always be br-int for now
mirrorname=$1
ports=$2
output=$3
vlan=${4:-}
sudo=sudo
[ "`id -u`" == 0 ] && sudo=
id=`uuidgen -t`

# Check port list
$echo $sudo ovs-vsctl --columns=ports list bridge $bridgename
tmp=`$sudo ovs-vsctl --columns=ports list bridge $bridgename 2>/dev/null`
if [ $? -ne 0 ]
then
	echo "tegu_add_mirror: $bridgename is missing on openvswitch." >&2
	exit 2	
fi
brports=`echo $tmp | sed 's/.*://' | tr -d '[] ' | tr , ' '`

realports=""
for p in `echo $ports | tr , ' '`
do
	case "$p" in
	*-*-*-*-*)
		# Port UUID
		if valid_port "$p"
		then
			realports="$realports,$p"
		else
			echo "tegu_add_mirror: there is no port with UUID=$p on $bridgename." >&2
			exit 2
		fi
		;;

	*:*:*:*:*:*)
		# MAC addr
		uuid=`translatemac $p`
		if valid_port "$uuid"
		then
			realports="$realports,$uuid"
		else
			echo "tegu_add_mirror: there is no port with MAC=$p on $bridgename." >&2
			exit 2
		fi
		;;

	*)
		echo "tegu_add_mirror: port $p is invalid (must be a UUID or a MAC)." >&2
		exit 2
		;;
	esac
done
realports=`echo $realports | sed 's/^,//'`

# Check output type
case "$output" in
vlan:[0-9]+)
	outputtype=vlan
	output=`echo $output | sed s/vlan://`
	;;

*.*.*.*)
	if valid_ip "$output"
	then
		outputtype=gre
		remoteip=$output
	else
		echo "tegu_add_mirror: $output is not a valid IP address." >&2
		exit 2
	fi
	;;

*-*-*-*-*)
	# Output port specified by UUID
	if valid_port "$output"
	then
		outputtype=port
	else
		echo "tegu_add_mirror: there is no port with UUID=$output on $bridgename." >&2
		exit 2
	fi
	;;

*:*:*:*:*:*)
	# MAC addr
	uuid=`translatemac $output`
	if valid_port "$uuid"
	then
		outputtype=port
		output="$uuid"
	else
		echo "tegu_add_mirror: there is no port with MAC=$output on $bridgename." >&2
		exit 2
	fi
	;;

*)
	echo "tegu_add_mirror: $output is not a valid output destination." >&2
	exit 2
	;;
esac

# Check VLANs (if any)
for v in `echo $vlan | tr , ' '`
do
	if [ "$v" -lt 0 -o "$v" -gt 4095 ]
	then
		echo "tegu_add_mirror: vlan $v is invalid (must be >= 0 and <= 4095)." >&2
		exit 2
	fi
done

# Generate arguments to ovs-vsctl
mirrorargs="select_src_port=$realports select_dst_port=$realports"
[ -n "$vlan" ] && mirrorargs="$mirrorargs select-vlan=$vlan"

case "$outputtype" in
gre)
	greportname=gre-$mirrorname
	$echo $sudo ovs-vsctl \
		add-port $bridgename $greportname \
		-- set interface $greportname type=gre options:remote_ip=$remoteip \
		-- --id=@p get port $greportname \
		-- --id=@m create mirror name=$mirrorname $mirrorargs output-port=@p \
		-- add bridge $bridgename mirrors @m
	$sudo ovs-vsctl \
		add-port $bridgename $greportname \
		-- set interface $greportname type=gre options:remote_ip=$remoteip \
		-- --id=@p get port $greportname \
		-- --id=@m create mirror name=$mirrorname $mirrorargs output-port=@p \
		-- add bridge $bridgename mirrors @m
	;;

vlan)
	$echo $sudo ovs-vsctl \
		--id=@m create mirror name=$mirrorname $mirrorargs output-vlan=$output \
		-- add bridge $bridgename mirrors @m
	$sudo ovs-vsctl \
		--id=@m create mirror name=$mirrorname $mirrorargs output-vlan=$output \
		-- add bridge $bridgename mirrors @m
	;;

port)
	$echo $sudo ovs-vsctl \
		-- --id=@p get port $output \
		-- --id=@m create mirror name=$mirrorname $mirrorargs output-port=@p \
		-- add bridge $bridgename mirrors @m
	$sudo ovs-vsctl \
		-- --id=@p get port $output \
		-- --id=@m create mirror name=$mirrorname $mirrorargs output-port=@p \
		-- add bridge $bridgename mirrors @m
	;;
esac

echo Mirror $mirrorname created.
exit 0
