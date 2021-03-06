#!/bin/bash

filename=$(basename "$0")
scriptname="${filename%.*}"
date "+${scriptname} start %Y%m%d_%H%M%S"

set -e
set -x
set -o pipefail
export SHELLOPTS

WALLET=$1

TEAL=test/scripts/e2e_subs/tealprogs

gcmd="goal -w ${WALLET}"

ACCOUNT=$(${gcmd} account list|awk '{ print $3 }')

APPID=$(${gcmd} app create --creator "${ACCOUNT}" --approval-prog=${TEAL}/state-rw.teal --global-byteslices 1 --global-ints 0 --local-byteslices 1 --local-ints 0  --clear-prog=${TEAL}/approve-all.teal | grep Created | awk '{ print $6 }')

function call {
    ${gcmd} app call --app-id=$APPID --from=$ACCOUNT --app-arg=str:$1  --app-arg=str:$2  --app-arg=str:$3  --app-arg=str:$4
}

set +o pipefail
call check global hello xyz 2>&1 | grep "cannot compare"
set -o pipefail

call write global hello xyz
call check global hello xyz


BIG64="1234567890123456789012345678901234567890123456789012345678901234"

call write global hello $BIG64
call check global hello $BIG64

set +o pipefail
call write global hello ${BIG64}X 2>&1 | grep "value too long"
set -o pipefail

date "+${scriptname} OK %Y%m%d_%H%M%S"
