load('ext://helm_remote', 'helm_remote')
load('ext://dotenv', 'dotenv')
dotenv()

# local resources
k8s_yaml([
    './cd/k8s/local/bind9-deployment.yaml'
])

k8s_resource(
   workload='bind9',
   port_forwards=['1053:53']
)

k8s_resource(
   workload='secondary',
   port_forwards=['1153:53']
)


k8s_yaml(helm(
    './cd/k8s/secondary',
    values=['./cd/k8s/secondary/values.yaml']
))