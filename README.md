# card-oci

This creates an OCI artifact representing a deck of playing cards. 

This helps show how layer de-duplication works, how any generic OCI artifact can be signed, and what an OCI implementation looks like with oras-go

Example local: 
```bash
./card-oci --deck=cards.json --local=my-local-deck
./card-oci --serve=my-local-deck
```

Example registry:
```bash
./card-oci --deck=cards.json --target=ghcr.io/austinabro321/card-deck:0.1.0
./card-oci --serve=ghcr.io/austinabro321/card-deck:0.1.0
```