idea: build a FreeBSD based infrastructure as a service

requirements:
  - api server (go based) that can create new instances based on existing golden images
    + launch instances
    + stop instances
    + terminate/destroy instances
    - create new instances based on snapshots of existing instances
    - basically, AWS EC2, Digital Ocean, vmware to a degree, entirely on-prem
    + puts config data into zfs properties
    - config file for network interfaces?
    - serial stuff
    - read iohyve/iocage for inspiration
    - support zvol images
    - support disk file based images
    - support nfsroot images
    - support FreeBSD guests
    - support Linux guests
    - support Windows guest
  - meta data server (go based) that services requests for configuration data from cloud init clients
    - serve correct instance data to instance based on zfs properties
  - cli interface to make requests to api server
    - allows settings per instance data
  - gui to make requests to api server
  - automated way to create images
  - some way to manage the instances across nodes
  - control access between instances

# list images
curl -i http://127.0.0.1:8080/api/v1/images

# launch instance from image (creates and starts)
curl -i -H 'Content-Type: application/json' -d '{"ima": "<imageid>", "mem": 512, "cpu": 1}' http://127.0.0.1:8080/api/v1/instances

# list instances
curl -i http://127.0.0.1:8080/api/v1/instances

# stop instance
curl -i -X PUT http://127.0.0.1:8080/api/v1/instances/<instanceid>

# start instance
curl -i -X POST http://127.0.0.1:8080/api/v1/instances/<instanceid>

# delete instance
curl -i -X DELETE http://127.0.0.1:8080/api/v1/instances/<instanceid>
