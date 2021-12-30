# App in a Box - Cost Sentry 

This is a simple solution that uses a Cloud Function to enforce Billing Budget constraints.
Namely when you go over budget, any Compute Engine instance with a label of `costsentry`
will be `stopped` by this system. It uses Pub/Sub to be the bridge between the Budget
and Cloud Functions

![Cost Sentry architecture](/architecture.png)

## Install
You can install this application using the `Open in Google Cloud Shell` button 
below. 

<a href="https://ssh.cloud.google.com/cloudshell/editor?cloudshell_git_repo=https%3A%2F%2Fgithub.com%2FGoogleCloudPlatform%2Fappinabox_costsentry&cloudshell_print=install.txt&shellonly=true">
        <img alt="Open in Cloud Shell" src="https://gstatic.com/cloudssh/images/open-btn.svg">
</a>

Once this opens up, you can install by: 
1. Creating a Google Cloud Project
1. Then typing `./install`

## Cleanup 
To remove all billing components from the project
1. Typing `./uninstall`


This is not an official Google product.