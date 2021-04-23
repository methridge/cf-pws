# Personal Weather Station Current Data

Modified version of my [CLI pws application](https://github.com/methridge/pws) 
to run in [Cloud Foundry](https://www.cloudfoundry.org/).  This also uses 
[HashiCorp Vault](https://www.vaultproject.io) with the 
[CF Auth](https://github.com/hashicorp/vault-plugin-auth-cf) method to retreive
secrets.

Reads the current conditions from Wunderground.com for my PWS.
