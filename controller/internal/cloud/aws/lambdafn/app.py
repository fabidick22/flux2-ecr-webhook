import json
import boto3
import os
import re
import urllib.request

secretsmanager = boto3.client('secretsmanager')
TOKEN_SECRET_NAME = os.environ['FLUX2_WEBHOOK_TOKEN_SECRET_NAME']
WEBHOOK_MAP_SECRET_NAME = os.environ['REPOS_MAPPING']
default_token = None
webhook_map = None


def get_global_token():
    global default_token
    if not default_token:
        response = secretsmanager.get_secret_value(SecretId=TOKEN_SECRET_NAME)
        default_token = response['SecretString']
    return default_token


def get_webhook_map():
    global webhook_map
    if not webhook_map:
        response = secretsmanager.get_secret_value(SecretId=WEBHOOK_MAP_SECRET_NAME)
        webhook_map = json.loads(response['SecretString'])
    return webhook_map


def process_ecr_push_event(detail):
    repository = detail['repository-name']
    image_digest = detail['image-digest']
    image_tag = detail['image-tag']

    print(json.dumps({
        'message': 'A new image has been pushed to the repository',
        'repository': repository,
        'image_digest': image_digest,
        'image_tag': image_tag
    }))


def make_request(webhook_url, repository, headers):
    if webhook_url:
        req = urllib.request.Request(
            webhook_url,
            data=json.dumps({}).encode('utf-8'),
            headers=headers,
            method='POST',
        )
        req.add_header('Content-Type', 'application/json')
        try:
            response = urllib.request.urlopen(req)
            print(json.dumps({
                'message': 'Webhook response',
                'status_code': response.status
            }))
        except urllib.error.HTTPError as e:
            print(json.dumps({
                'message': 'Webhook request failed',
                'status_code': e.code,
                'reason': str(e.reason)
            }))
    else:
        print(json.dumps({
            'message': 'No webhook endpoint found for repository',
            'repository': repository
        }))


def call_flux_webhook(repository, image_tag):
    wh_map = get_webhook_map()

    if repository in wh_map:
        repo_data = wh_map[repository]
        for key, data in repo_data.items():
            webhook_urls = data.get('webhook', [])
            token = data.get('token', get_global_token())
            regex = data.get('regex', '.*')
            for webhook in webhook_urls:
                headers = {'Authorization': f'Bearer {token}'}
                if regex and re.match(regex, image_tag):
                    make_request(webhook, repository, headers)
                else:
                    print(json.dumps({
                        'message': f'Tag {image_tag} does not match regex ({regex})',
                        'repository': repository
                    }))


def lambda_handler(event, context):
    record = event['Records'][0]
    message = json.loads(record['body'])
    detail = message['detail']

    process_ecr_push_event(detail)
    call_flux_webhook(detail['repository-name'], detail['image-tag'])

    return {
        'statusCode': 200,
        'body': json.dumps('Event processed successfully')
    }
