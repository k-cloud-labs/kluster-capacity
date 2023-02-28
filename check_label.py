from github import Github
import os
import sys

# GitHub API 认证
g = Github(os.environ['GITHUB_TOKEN'])
repo = g.get_repo(os.environ['GITHUB_REPOSITORY'])

# 获取 repository 下的所有标签
labels = [label.name for label in repo.get_labels()]

if sys.argv[1] == 'issues':
    issue_number = sys.argv[2]
    issue = repo.get_issue(int(issue_number))
    issue_labels = [label.name for label in issue.labels]

    # 判断 issue 是否包含所有标签中的其中之一
    if not set(labels).intersection(set(issue_labels)):
        message = "Please add a label from the following list: " + str(labels)
        issue.create_comment(message)

if sys.argv[1] == 'pull_request':
    pull_request_number = sys.argv[2]
    pull_request = repo.get_pull(int(pull_request_number))
    pull_request_labels = [label.name for label in pull_request.labels]

    # 判断 pull_request 是否包含所有标签中的其中之一
    if not set(labels).intersection(set(pull_request_labels)):
        message = "Please add a label from the following list: " + str(labels)
        pull_request.create_issue_comment(message)

        # 自动添加一个标明大小的标签
        size_labels = ["size/S", "size/M", "size/L", "size/XL"]
        lines_of_code = pull_request.additions + pull_request.deletions
        if lines_of_code <= 50:
            pull_request.add_to_labels(size_labels[0])
        elif lines_of_code <= 100:
            pull_request.add_to_labels(size_labels[1])
        elif lines_of_code <= 500:
            pull_request.add_to_labels(size_labels[2])
        else:
            pull_request.add_to_labels(size_labels[3])
