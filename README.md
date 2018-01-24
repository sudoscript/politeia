# *Immutable Campaign Promises*

Politicians running for office make a lot of promises. Few are rarely ever kept.

This prototype uses [Politeia](https://github.com/decred/politeia), the Decred proposal system, to hold politicians accountable to the promises they made on the campaign trail.

* Anyone can submit a policy idea. They are vetted by a governing body, such as an election commission.
* Candidates running for office bundle groups of vetted policy ideas into slates of campaign promises.
* Slates are signed by the candidate and identified with their public key. Only they can create or change them.
* The governing body can finalize slates, after which they can no longer be changed.
* Merkle trees guarantee that campaign promises cannot be changed or removed from the slate.
* Before the next election, voters can validate which promises were kept and which were broken.

## Install and Run

Clone this repo. You'll need to save it to the regular Politeia directory to maintain package linkages, so be sure to save or push local changes before cloning.
```
$ git clone git@gitlab.com:jarins/politeia.git $GOPATH/src/github.com/decred/politeia
```

Then compile and launch the politeia daemon:
```
$ cd $GOPATH/src/github.com/decred/politeia
$ git checkout campaign_promises
$ dep ensure && go install -v ./... && LOGFLAGS=shortfile politeiad --testnet --rpcuser=user --rpcpass=pass
```

Download server identity to client:
```
$ politeia -v -testnet -rpchost 127.0.0.1 identity
```
Accept default path by pressing `enter`.


### Propose Policy Ideas

Use the CLI to publish a few campaign promises:
```
$ echo "No new taxes" > read_my_lips.txt
$ politeia -v -testnet -rpchost 127.0.0.1 new read_my_lips.txt
00: b209addb8482d4e761ab64daeafb2807a3ee2fb2a5e086a69ec98ab2663a8ec4 read_my_lips.txt text/plain; charset=utf-8
Record submitted
  Censorship record:
    Merkle   : b209addb8482d4e761ab64daeafb2807a3ee2fb2a5e086a69ec98ab2663a8ec4
    Token    : da90c356684dd113e17625b7232975320cfa9bacbc0ac15057270df52c4fa3f4
    Signature: 3a54e7cba53f841f3a92907db887db1cf75b000dc372e89fc97acdde6121d426a0ad7f9e1b3be2b2e403c1a370234e76ad8fcd3496ee2f5121df3f4091a7900e

$ echo "No new wars" > peace_in_our_time.txt
$ politeia -v -testnet -rpchost 127.0.0.1 new peace_in_our_time.txt
00: a5a3cd6ffe93c64bb010f7cde4d89946516059eaf3fe5bb45b10ffa75e0e3ecb peace_in_our_time.txt text/plain; charset=utf-8
Record submitted
  Censorship record:
    Merkle   : a5a3cd6ffe93c64bb010f7cde4d89946516059eaf3fe5bb45b10ffa75e0e3ecb
    Token    : 3ccb5154c7d214fb671bd8025dc771ed38f0ac84592e7dd1c6b006497aebdea8
    Signature: 51af970f819a2f9333d5b98fd69630d0a7954520b2aea89497ada6c3d75d3b36bb303541a2f99a86a0b9b15b1bbfb76d91f02aa3db9cae41d31f947bcde68004

$ echo "Create 100K new jobs" > its_economy_stupid.txt
$ politeia -v -testnet -rpchost 127.0.0.1 new its_economy_stupid.txt
00: 6e82da5a3c62c9866f2937456ee63d08d5e4eda453cf033ac3a68e747cb9c8fe its_economy_stupid.txt text/plain; charset=utf-8
Record submitted
  Censorship record:
    Merkle   : 6e82da5a3c62c9866f2937456ee63d08d5e4eda453cf033ac3a68e747cb9c8fe
    Token    : 02677541829a9bec6dfcede4e6b70358defd994676ad0e3d58044e775305ca1c
    Signature: 8668f2bdb9110ba53bd23f1042b51243192c3a11d9f8dbaca4be85c2db2c43163707e8a1193796f08066085f0884d7fa204ba2b13dabe1b548177835d3bf5002
```

Acting as the election commission, verify the policy proposals as valid (this requires `-rpcuser=user` and `-rpcpas=pass`).
```
$ politeia -v -testnet -rpchost 127.0.0.1 -rpcuser user -rpcpass pass setunvettedstatus publish da90c356684dd113e17625b7232975320cfa9bacbc0ac15057270df52c4fa3f4
Set record status:
  Status   : public

$ politeia -v -testnet -rpchost 127.0.0.1 -rpcuser user -rpcpass pass setunvettedstatus publish 3ccb5154c7d214fb671bd8025dc771ed38f0ac84592e7dd1c6b006497aebdea8
Set record status:
  Status   : public

$ politeia -v -testnet -rpchost 127.0.0.1 -rpcuser user -rpcpass pass setunvettedstatus publish 02677541829a9bec6dfcede4e6b70358defd994676ad0e3d58044e775305ca1c
Set record status:
  Status   : public
```

### Create a Campaign Platform

Now candidates can combine policy ideas into slates with the `newslate` command, followed by a list of space-separated vetted proposals. In order to sign the slate and prove authorship, they need to pass a user identity file with the `-user` flag. If the filename doesn't exist, it'll be created for them.
```
$ politeia -v -testnet -rpchost 127.0.0.1 -user MrPopular.json newslate da90c356684dd113e17625b7232975320cfa9bacbc0ac15057270df52c4fa3f4 3ccb5154c7d214fb671bd8025dc771ed38f0ac84592e7dd1c6b006497aebdea8 02677541829a9bec6dfcede4e6b70358defd994676ad0e3d58044e775305ca1c
Identity file found.
  Censorship record:
    Merkle   : e74acc2d17f0e697a0fe52551fd0736f0979bff6f5300e6dbd302f0cccf8f357
    Token    : 6a6f55d8db19c6736865592848f81c2bbe9aeb727a59b076b2d7d9960ae2b9b5
    Signature: 95161f36dc2721f3c74275d8cad121c78222cce0b6fc1caedecfa8240072ffeaa55178d6ad37d6dbf66612476afae00a953dbd20cd144ebffaba1681a79b740e
```

Voters can see the slates, their component promises, and the public key of the candidate who published it
```
$ politeia -v -testnet -rpchost 127.0.0.1 getslate 6a6f55d8db19c6736865592848f81c2bbe9aeb727a59b076b2d7d9960ae2b9b5
Electoral Slate
Owner's Public Key: 82b70ed135f6b9b5f581a7a0fa5becc9d75d57fce26c6ea2572ec78aa4532267
  Censorship record:
    Merkle   : e74acc2d17f0e697a0fe52551fd0736f0979bff6f5300e6dbd302f0cccf8f357
    Token    : 6a6f55d8db19c6736865592848f81c2bbe9aeb727a59b076b2d7d9960ae2b9b5
    Signature: 95161f36dc2721f3c74275d8cad121c78222cce0b6fc1caedecfa8240072ffeaa55178d6ad37d6dbf66612476afae00a953dbd20cd144ebffaba1681a79b740e

Electoral promise 00:
  Status     : public
  Timestamp  : 2018-01-24 03:46:13 +0000 UTC
  Censorship record:
    Merkle   : 6e82da5a3c62c9866f2937456ee63d08d5e4eda453cf033ac3a68e747cb9c8fe
    Token    : 02677541829a9bec6dfcede4e6b70358defd994676ad0e3d58044e775305ca1c
    Signature: 8668f2bdb9110ba53bd23f1042b51243192c3a11d9f8dbaca4be85c2db2c43163707e8a1193796f08066085f0884d7fa204ba2b13dabe1b548177835d3bf5002
      Metadata   : []
  File (00)  :
    Name     : its_economy_stupid.txt
    MIME     : text/plain; charset=utf-8
    Digest   : 6e82da5a3c62c9866f2937456ee63d08d5e4eda453cf033ac3a68e747cb9c8fe

Electoral promise 01:
  Status     : public
  Timestamp  : 2018-01-24 03:46:06 +0000 UTC
  Censorship record:
    Merkle   : a5a3cd6ffe93c64bb010f7cde4d89946516059eaf3fe5bb45b10ffa75e0e3ecb
    Token    : 3ccb5154c7d214fb671bd8025dc771ed38f0ac84592e7dd1c6b006497aebdea8
    Signature: 51af970f819a2f9333d5b98fd69630d0a7954520b2aea89497ada6c3d75d3b36bb303541a2f99a86a0b9b15b1bbfb76d91f02aa3db9cae41d31f947bcde68004
  Metadata   : []
  File (00)  :
    Name     : peace_in_our_time.txt
    MIME     : text/plain; charset=utf-8
    Digest   : a5a3cd6ffe93c64bb010f7cde4d89946516059eaf3fe5bb45b10ffa75e0e3ecb

Electoral promise 02:
  Status     : public
  Timestamp  : 2018-01-24 03:36:05 +0000 UTC
  Censorship record:
    Merkle   : b209addb8482d4e761ab64daeafb2807a3ee2fb2a5e086a69ec98ab2663a8ec4
    Token    : da90c356684dd113e17625b7232975320cfa9bacbc0ac15057270df52c4fa3f4
    Signature: 3a54e7cba53f841f3a92907db887db1cf75b000dc372e89fc97acdde6121d426a0ad7f9e1b3be2b2e403c1a370234e76ad8fcd3496ee2f5121df3f4091a7900e
  Metadata   : []
  File (00)  :
    Name     : read_my_lips.txt
    MIME     : text/plain; charset=utf-8
    Digest   : b209addb8482d4e761ab64daeafb2807a3ee2fb2a5e086a69ec98ab2663a8ec4
```

During the campaign, candidates can edit slates, by proving they hold the private key corresponding to it.
```
$ politeia -v -testnet -rpchost 127.0.0.1 -user MrPopular.json updateslate 6a6f55d8db19c6736865592848f81c2bbe9aeb727a59b076b2d7d9960ae2b9b5 del:da90c356684dd113e17625b7232975320cfa9bacbc0ac15057270df52c4fa3f4
Identity file found.
  Censorship record:
    Merkle   : 9c8418bec3169bc1124dcd30e19132b127b816a3ced231f6589cd426713dcd59
    Token    : 6a6f55d8db19c6736865592848f81c2bbe9aeb727a59b076b2d7d9960ae2b9b5
    Signature: 0d0f0684557d0bd20284d29054964df07d728b47d43012cea9d0ba98e7491641e083dd3055b2168118eb47139db1fa8ca2c02dc3434a785256d3da5afdf28507
```

### Finalize Campaign Promises in Cryptographic Immutability

When it gets close to election day, the election commission freezes the slates.
```
$ politeia -v -testnet -rpchost 127.0.0.1 -rpcuser user -rpcpass pass setunvettedstatus publish 6a6f55d8db19c6736865592848f81c2bbe9aeb727a59b076b2d7d9960ae2b9b5
Set record status:
  Status   : public
```

After that, promises are immutable, and can no longer be added or removed by the candidates.
```
$ politeia -v -testnet -rpchost 127.0.0.1 -user MrPopular.json updateslate 6a6f55d8db19c6736865592848f81c2bbe9aeb727a59b076b2d7d9960ae2b9b5 del:3ccb5154c7d214fb671bd8025dc771ed38f0ac84592e7dd1c6b006497aebdea8
Identity file found.
400 Bad Request: invalid request payload:
```

Voters can continue to see published slates, and can vote (on-chain?!) to verify whether the promises have been kept.
```
$ politeia -v -testnet -rpchost 127.0.0.1 getslate 6a6f55d8db19c6736865592848f81c2bbe9aeb727a59b076b2d7d9960ae2b9b5
Electoral Slate
Owner's Public Key: 82b70ed135f6b9b5f581a7a0fa5becc9d75d57fce26c6ea2572ec78aa4532267
  Censorship record:
    Merkle   : 9c8418bec3169bc1124dcd30e19132b127b816a3ced231f6589cd426713dcd59
    Token    : 6a6f55d8db19c6736865592848f81c2bbe9aeb727a59b076b2d7d9960ae2b9b5
    Signature: 0d0f0684557d0bd20284d29054964df07d728b47d43012cea9d0ba98e7491641e083dd3055b2168118eb47139db1fa8ca2c02dc3434a785256d3da5afdf28507

Electoral promise 00:
  Status     : public
  Timestamp  : 2018-01-24 03:46:13 +0000 UTC
  Censorship record:
    Merkle   : 6e82da5a3c62c9866f2937456ee63d08d5e4eda453cf033ac3a68e747cb9c8fe
    Token    : 02677541829a9bec6dfcede4e6b70358defd994676ad0e3d58044e775305ca1c
    Signature: 8668f2bdb9110ba53bd23f1042b51243192c3a11d9f8dbaca4be85c2db2c43163707e8a1193796f08066085f0884d7fa204ba2b13dabe1b548177835d3bf5002
  Metadata   : []
  File (00)  :
    Name     : its_economy_stupid.txt
    MIME     : text/plain; charset=utf-8
    Digest   : 6e82da5a3c62c9866f2937456ee63d08d5e4eda453cf033ac3a68e747cb9c8fe

Electoral promise 01:
  Status     : public
  Timestamp  : 2018-01-24 03:46:06 +0000 UTC
  Censorship record:
    Merkle   : a5a3cd6ffe93c64bb010f7cde4d89946516059eaf3fe5bb45b10ffa75e0e3ecb
    Token    : 3ccb5154c7d214fb671bd8025dc771ed38f0ac84592e7dd1c6b006497aebdea8
    Signature: 51af970f819a2f9333d5b98fd69630d0a7954520b2aea89497ada6c3d75d3b36bb303541a2f99a86a0b9b15b1bbfb76d91f02aa3db9cae41d31f947bcde68004
  Metadata   : []
  File (00)  :
    Name     : peace_in_our_time.txt
    MIME     : text/plain; charset=utf-8
    Digest   : a5a3cd6ffe93c64bb010f7cde4d89946516059eaf3fe5bb45b10ffa75e0e3ecb
```

